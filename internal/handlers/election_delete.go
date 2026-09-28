package handlers

import (
	"database/sql"
	"strconv"

	"evoting/internal/database"

	"github.com/gofiber/fiber/v2"
)

func itoa(n int) string { return strconv.Itoa(n) }

// DeleteElection permanently removes an election together with its candidates
// and every vote cast in it. An active election must be deactivated first, so
// a stray click can't wipe a live vote.
// DELETE /api/admin/elections/:id
func DeleteElection(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)

	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid election id"})
	}

	var title string
	var active bool
	err = database.DB.QueryRow(`SELECT title, is_active FROM elections WHERE id = $1`, id).Scan(&title, &active)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Election not found"})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Could not load election"})
	}
	if active {
		return c.Status(409).JSON(fiber.Map{"error": "Deactivate this election before deleting it"})
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to start deletion"})
	}
	defer tx.Rollback()

	// votes reference both elections and candidates without ON DELETE CASCADE,
	// so they have to go first. Candidates then cascade with the election.
	res, err := tx.Exec(`DELETE FROM votes WHERE election_id = $1`, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to delete votes"})
	}
	votesDeleted, _ := res.RowsAffected()

	if _, err := tx.Exec(`DELETE FROM elections WHERE id = $1`, id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to delete election"})
	}
	if err := tx.Commit(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to delete election"})
	}

	logAudit(adminID, "ELECTION_DELETED",
		"Deleted election \""+title+"\" (id "+itoa(id)+") and "+strconv.FormatInt(votesDeleted, 10)+" votes", c.IP())

	return c.JSON(fiber.Map{"message": "Election deleted", "votes_deleted": votesDeleted})
}
