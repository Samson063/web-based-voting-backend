package handlers

import (
	"evoting/internal/database"
	"evoting/internal/models"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
)

// CreateElection creates a new election (admin only)
func CreateElection(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)

	var body map[string]interface{}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	title, _ := body["title"].(string)
	description, _ := body["description"].(string)
	startStr, _ := body["start_time"].(string)
	endStr, _ := body["end_time"].(string)
	isActive, _ := body["is_active"].(bool)

	if title == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Election title is required"})
	}
	if startStr == "" || endStr == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Start and end times are required"})
	}

	layouts := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
	}

	parseTime := func(s string) (time.Time, error) {
		for _, layout := range layouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("cannot parse date: %s", s)
	}

	startTime, err := parseTime(startStr)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid start_time format. Use YYYY-MM-DDTHH:MM"})
	}
	endTime, err := parseTime(endStr)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid end_time format. Use YYYY-MM-DDTHH:MM"})
	}

	var id int
	err = database.DB.QueryRow(`
		INSERT INTO elections (title, description, start_time, end_time, is_active)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, title, description, startTime, endTime, isActive).Scan(&id)

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to create election: " + err.Error()})
	}

	logAudit(adminID, "CREATE_ELECTION", fmt.Sprintf("Election '%s' created with ID %d", title, id), c.IP())

	return c.Status(201).JSON(fiber.Map{"message": "Election created", "id": id})
}

// ToggleElection activates or deactivates an election
func ToggleElection(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)
	electionID := c.Params("id")

	var isActive bool
	database.DB.QueryRow(`SELECT is_active FROM elections WHERE id = $1`, electionID).Scan(&isActive)

	_, err := database.DB.Exec(`UPDATE elections SET is_active = $1 WHERE id = $2`, !isActive, electionID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to update election"})
	}

	status := "activated"
	if isActive {
		status = "deactivated"
	}
	logAudit(adminID, "TOGGLE_ELECTION", fmt.Sprintf("Election %s %s", electionID, status), c.IP())

	return c.JSON(fiber.Map{"message": fmt.Sprintf("Election %s", status), "is_active": !isActive})
}

// AddCandidate adds a candidate to an election
func AddCandidate(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)

	var cand models.Candidate
	if err := c.BodyParser(&cand); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid request body"})
	}

	if cand.FullName == "" || cand.Position == "" || cand.ElectionID == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Name, position, and election_id are required"})
	}

	var id int
	err := database.DB.QueryRow(`
		INSERT INTO candidates (election_id, full_name, position, department, manifesto)
		VALUES ($1, $2, $3, $4, $5) RETURNING id
	`, cand.ElectionID, cand.FullName, cand.Position, cand.Department, cand.Manifesto).Scan(&id)

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to add candidate"})
	}

	logAudit(adminID, "ADD_CANDIDATE", fmt.Sprintf("Candidate '%s' added for position '%s'", cand.FullName, cand.Position), c.IP())

	return c.Status(201).JSON(fiber.Map{"message": "Candidate added", "id": id})
}

// GetAllUsers returns all registered voters (admin only)
func GetAllUsers(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT id, matric_number, full_name, email, department, role, is_eligible, has_voted, created_at
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch users"})
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		rows.Scan(&u.ID, &u.MatricNumber, &u.FullName, &u.Email, &u.Department,
			&u.Role, &u.IsEligible, &u.HasVoted, &u.CreatedAt)
		users = append(users, u)
	}

	if users == nil {
		users = []models.User{}
	}
	return c.JSON(users)
}

// GetAuditLogs returns the audit trail (admin only)
func GetAuditLogs(c *fiber.Ctx) error {
	rows, err := database.DB.Query(`
		SELECT al.id, al.user_id, al.action, al.details, al.ip_address, al.created_at,
		       COALESCE(u.matric_number, 'system') as matric
		FROM audit_logs al
		LEFT JOIN users u ON u.id = al.user_id
		ORDER BY al.created_at DESC LIMIT 200
	`)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to fetch audit logs"})
	}
	defer rows.Close()

	type LogEntry struct {
		models.AuditLog
		Matric string `json:"matric_number"`
	}

	var logs []LogEntry
	for rows.Next() {
		var l LogEntry
		rows.Scan(&l.ID, &l.UserID, &l.Action, &l.Details, &l.IPAddress, &l.CreatedAt, &l.Matric)
		logs = append(logs, l)
	}

	if logs == nil {
		logs = []LogEntry{}
	}
	return c.JSON(logs)
}

// GetDashboardStats returns summary numbers for the admin dashboard
func GetDashboardStats(c *fiber.Ctx) error {
	var totalVoters, totalVoted, totalElections, totalCandidates int

	database.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'voter'`).Scan(&totalVoters)
	database.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE has_voted = TRUE`).Scan(&totalVoted)
	database.DB.QueryRow(`SELECT COUNT(*) FROM elections`).Scan(&totalElections)
	database.DB.QueryRow(`SELECT COUNT(*) FROM candidates`).Scan(&totalCandidates)

	turnout := 0.0
	if totalVoters > 0 {
		turnout = float64(totalVoted) / float64(totalVoters) * 100
	}

	return c.JSON(fiber.Map{
		"total_voters":     totalVoters,
		"total_voted":      totalVoted,
		"total_elections":  totalElections,
		"total_candidates": totalCandidates,
		"turnout_percent":  fmt.Sprintf("%.1f", turnout),
	})
}

// GetPublicStats returns basic counts for the public landing page — no login required
func GetPublicStats(c *fiber.Ctx) error {
	var totalVoters, totalElections, totalCandidates, totalVoted int

	database.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&totalVoters)
	database.DB.QueryRow(`SELECT COUNT(*) FROM elections`).Scan(&totalElections)
	database.DB.QueryRow(`SELECT COUNT(*) FROM candidates`).Scan(&totalCandidates)
	database.DB.QueryRow(`SELECT COUNT(*) FROM votes`).Scan(&totalVoted)

	return c.JSON(fiber.Map{
		"total_voters":     totalVoters,
		"total_elections":  totalElections,
		"total_candidates": totalCandidates,
		"total_voted":      totalVoted,
	})
}

// SetEligibility updates a voter's eligibility status
func SetEligibility(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)
	userID := c.Params("id")

	var body struct {
		IsEligible bool `json:"is_eligible"`
	}
	c.BodyParser(&body)

	_, err := database.DB.Exec(`UPDATE users SET is_eligible = $1 WHERE id = $2`, body.IsEligible, userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to update eligibility"})
	}

	logAudit(adminID, "SET_ELIGIBILITY", fmt.Sprintf("User %s eligibility set to %v", userID, body.IsEligible), c.IP())

	return c.JSON(fiber.Map{"message": "Eligibility updated"})
}

// logAudit is a helper used by all handlers to write audit records
func logAudit(userID int, action, details, ip string) {
	database.DB.Exec(`
		INSERT INTO audit_logs (user_id, action, details, ip_address)
		VALUES ($1, $2, $3, $4)
	`, userID, action, details, ip)
}
