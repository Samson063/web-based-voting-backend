package handlers

import (
	"database/sql"
	"strings"
	"unicode"

	"evoting/internal/database"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// Roster check used by Register
// ---------------------------------------------------------------------------

// checkRoster decides whether a sign-up may proceed.
//
//   - Roster empty            -> registration is open (nothing to check against)
//   - Matric not on roster    -> refused
//   - Name doesn't resemble   -> refused (stops someone claiming another
//     the roster name           student's matric number)
//
// It returns the department recorded on the roster so the account uses the
// official value instead of whatever was typed.
func checkRoster(matric, fullName string) (department string, allowed bool, reason string) {
	var total int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM student_roster`).Scan(&total); err != nil {
		return "", false, "Could not verify student records. Please try again."
	}
	if total == 0 {
		return "", true, ""
	}

	var rosterName, dept string
	err := database.DB.QueryRow(`
		SELECT full_name, department FROM student_roster
		WHERE UPPER(matric_number) = UPPER($1)
	`, matric).Scan(&rosterName, &dept)

	if err == sql.ErrNoRows {
		return "", false, "This matric number was not found in the BOUESTI student records. Contact the election committee if you think this is a mistake."
	}
	if err != nil {
		return "", false, "Could not verify student records. Please try again."
	}

	if !namesMatch(rosterName, fullName) {
		return "", false, "The name you entered does not match the student records for this matric number."
	}
	return dept, true, ""
}

func nameTokens(s string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		out[f] = true
	}
	return out
}

// namesMatch is deliberately forgiving: order doesn't matter and a middle name
// may be missing, but at least two of the roster's name parts must appear.
func namesMatch(rosterName, submitted string) bool {
	rt := nameTokens(rosterName)
	if len(rt) == 0 {
		return true // roster row has no name to compare against
	}
	st := nameTokens(submitted)

	hits := 0
	for t := range rt {
		if st[t] {
			hits++
		}
	}
	need := 2
	if len(rt) < 2 {
		need = len(rt)
	}
	return hits >= need
}

// ---------------------------------------------------------------------------
// Admin endpoints
// ---------------------------------------------------------------------------

// RosterSummary reports how many students are on the roster.
// GET /api/admin/roster
func RosterSummary(c *fiber.Ctx) error {
	var n int
	if err := database.DB.QueryRow(`SELECT COUNT(*) FROM student_roster`).Scan(&n); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to read roster"})
	}
	return c.JSON(fiber.Map{"count": n, "enforced": n > 0})
}

type rosterEntry struct {
	MatricNumber string `json:"matric_number"`
	FullName     string `json:"full_name"`
	Department   string `json:"department"`
}

// UploadRoster adds or updates students. Existing entries with the same matric
// number are overwritten, so re-uploading a corrected list is safe.
// POST /api/admin/roster   { "students": [ {matric_number, full_name, department}, ... ] }
func UploadRoster(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)

	var body struct {
		Students []rosterEntry `json:"students"`
	}
	if err := c.BodyParser(&body); err != nil || len(body.Students) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "No students provided"})
	}
	if len(body.Students) > 20000 {
		return c.Status(400).JSON(fiber.Map{"error": "Too many rows in one upload (max 20,000)"})
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to start upload"})
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO student_roster (matric_number, full_name, department)
		VALUES ($1, $2, $3)
		ON CONFLICT (matric_number) DO UPDATE
		  SET full_name = EXCLUDED.full_name, department = EXCLUDED.department
	`)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to prepare upload"})
	}
	defer stmt.Close()

	saved, skipped := 0, 0
	for _, s := range body.Students {
		m := strings.TrimSpace(s.MatricNumber)
		if m == "" || len(m) > 20 {
			skipped++
			continue
		}
		if _, err := stmt.Exec(m, strings.TrimSpace(s.FullName), strings.TrimSpace(s.Department)); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "Failed to save roster"})
		}
		saved++
	}

	if err := tx.Commit(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to save roster"})
	}

	logAudit(adminID, "ROSTER_UPLOADED", "Student roster upload: "+itoa(saved)+" saved, "+itoa(skipped)+" skipped", c.IP())
	return c.JSON(fiber.Map{"saved": saved, "skipped": skipped})
}

// ClearRoster empties the roster, which reopens registration to everyone.
// DELETE /api/admin/roster
func ClearRoster(c *fiber.Ctx) error {
	adminID := c.Locals("userID").(int)
	if _, err := database.DB.Exec(`DELETE FROM student_roster`); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Failed to clear roster"})
	}
	logAudit(adminID, "ROSTER_CLEARED", "Student roster cleared — registration is open to all", c.IP())
	return c.JSON(fiber.Map{"message": "Roster cleared"})
}
