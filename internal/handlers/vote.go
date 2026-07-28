package handlers

import (
	"crypto/sha256"
	"database/sql"
	"evoting/internal/database"
	"evoting/internal/models"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

// GetElections returns all elections (active or all for admin)
func GetElections(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT id, title, description, start_time, end_time, is_active, created_at
		FROM elections ORDER BY created_at DESC
	`)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch elections"})
	}
	defer rows.Close()

	var elections []models.Election
	for rows.Next() {
		var e models.Election
		rows.Scan(&e.ID, &e.Title, &e.Description, &e.StartTime, &e.EndTime, &e.IsActive, &e.CreatedAt)
		elections = append(elections, e)
	}

	if elections == nil {
		elections = []models.Election{}
	}
	return c.JSON(elections)
}

// GetCandidates returns candidates for a specific election
func GetCandidates(c *fiber.Ctx) error {
	electionID := c.Params("election_id")

	rows, err := database.DB.Query(`
		SELECT id, election_id, full_name, position, department, manifesto, vote_count
		FROM candidates WHERE election_id = $1 ORDER BY position, full_name
	`, electionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch candidates"})
	}
	defer rows.Close()

	var candidates []models.Candidate
	for rows.Next() {
		var cand models.Candidate
		rows.Scan(&cand.ID, &cand.ElectionID, &cand.FullName, &cand.Position,
			&cand.Department, &cand.Manifesto, &cand.VoteCount)
		candidates = append(candidates, cand)
	}

	if candidates == nil {
		candidates = []models.Candidate{}
	}
	return c.JSON(candidates)
}

// CastVote records a vote — the core function of the system
// The database UNIQUE constraint ensures one student one vote at the DB level
func CastVote(c *fiber.Ctx) error {
	userID := c.Locals("userID").(int)

	var req models.CastVoteRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// --- Step 1: Check voter eligibility ---
	var isEligible, hasVoted bool
	err := database.DB.QueryRow(`
		SELECT is_eligible, has_voted FROM users WHERE id = $1
	`, userID).Scan(&isEligible, &hasVoted)

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not verify eligibility"})
	}
	if !isEligible {
		return c.Status(403).JSON(fiber.Map{"error": "You are not eligible to vote in this election"})
	}
	if hasVoted {
		return c.Status(409).JSON(fiber.Map{"error": "You have already cast your vote"})
	}

	// --- Step 2: Check that election is active ---
	var election models.Election
	err = database.DB.QueryRow(`
		SELECT id, is_active, start_time, end_time FROM elections WHERE id = $1
	`, req.ElectionID).Scan(&election.ID, &election.IsActive, &election.StartTime, &election.EndTime)

	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Election not found"})
	}
	now := time.Now().UTC()
	if !election.IsActive || now.Before(election.StartTime.UTC()) || now.After(election.EndTime.UTC()) {
		return c.Status(403).JSON(fiber.Map{"error": "This election is not currently open for voting"})
	}

	// --- Step 3: Use a database TRANSACTION to safely record the vote ---
	// A transaction ensures both operations succeed or both fail together
	tx, err := database.DB.Begin()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to begin transaction"})
	}

	// Generate a unique vote receipt (hash of userID + electionID + timestamp)
	receiptData := fmt.Sprintf("%d-%d-%d-%s", userID, req.ElectionID, req.CandidateID, time.Now().String())
	receipt := fmt.Sprintf("%x", sha256.Sum256([]byte(receiptData)))

	// Insert the vote
	_, err = tx.Exec(`
		INSERT INTO votes (user_id, election_id, candidate_id, vote_receipt)
		VALUES ($1, $2, $3, $4)
	`, userID, req.ElectionID, req.CandidateID, receipt)

	if err != nil {
		tx.Rollback()
		return c.Status(409).JSON(fiber.Map{"error": "Vote already recorded or invalid candidate"})
	}

	// Increment candidate vote count
	_, err = tx.Exec(`
		UPDATE candidates SET vote_count = vote_count + 1 WHERE id = $1
	`, req.CandidateID)

	if err != nil {
		tx.Rollback()
		return c.Status(500).JSON(fiber.Map{"error": "Failed to update vote count"})
	}

	// Mark user as having voted
	_, err = tx.Exec(`UPDATE users SET has_voted = TRUE WHERE id = $1`, userID)
	if err != nil {
		tx.Rollback()
		return c.Status(500).JSON(fiber.Map{"error": "Failed to update voter status"})
	}

	// Commit the transaction — all or nothing
	if err = tx.Commit(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to record vote"})
	}

	// Log for audit trail
	logAudit(userID, "VOTE_CAST", fmt.Sprintf("Vote cast in election %d, receipt: %s", req.ElectionID, receipt[:16]+"..."), c.IP())

	return c.Status(201).JSON(models.CastVoteResponse{
		Message:     "Your vote has been recorded successfully!",
		VoteReceipt: receipt,
	})
}

// GetResults returns election results with vote counts
func GetResults(c *fiber.Ctx) error {
	electionID := c.Params("election_id")

	// Get the election details
	var election models.Election
	err := database.DB.QueryRow(`
		SELECT id, title, description, start_time, end_time, is_active, created_at
		FROM elections WHERE id = $1
	`, electionID).Scan(
		&election.ID, &election.Title, &election.Description,
		&election.StartTime, &election.EndTime, &election.IsActive, &election.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Election not found"})
	}

	// Get candidates with vote counts
	rows, err := database.DB.Query(`
		SELECT id, election_id, full_name, position, department, manifesto, vote_count
		FROM candidates WHERE election_id = $1 ORDER BY position, vote_count DESC
	`, electionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch results"})
	}
	defer rows.Close()

	var candidates []models.Candidate
	totalVotes := 0
	for rows.Next() {
		var cand models.Candidate
		rows.Scan(&cand.ID, &cand.ElectionID, &cand.FullName, &cand.Position,
			&cand.Department, &cand.Manifesto, &cand.VoteCount)
		totalVotes += cand.VoteCount
		candidates = append(candidates, cand)
	}

	if candidates == nil {
		candidates = []models.Candidate{}
	}

	return c.JSON(models.ElectionResult{
		Election:   election,
		Candidates: candidates,
		TotalVotes: totalVotes,
	})
}

// VerifyReceipt lets a voter verify their vote was counted
func VerifyReceipt(c *fiber.Ctx) error {
	receipt := c.Params("receipt")

	var vote models.Vote
	err := database.DB.QueryRow(`
		SELECT id, election_id, candidate_id, vote_receipt, cast_at
		FROM votes WHERE vote_receipt = $1
	`, receipt).Scan(&vote.ID, &vote.ElectionID, &vote.CandidateID, &vote.VoteReceipt, &vote.CastAt)

	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{
			"valid":   false,
			"message": "Receipt not found — vote may not have been recorded",
		})
	}

	return c.JSON(fiber.Map{
		"valid":       true,
		"election_id": vote.ElectionID,
		"cast_at":     vote.CastAt,
		"message":     "Your vote is confirmed in the system",
	})
}
