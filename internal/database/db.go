package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq" // PostgreSQL driver
)

var DB *sql.DB

// Connect establishes a connection to PostgreSQL
func Connect() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Default local dev connection
		dsn = "host=localhost user=postgres password=Astrocoder1 dbname=E-voting sslmode=disable"
	}

	var err error
	DB, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("Database connected successfully")
}

// CreateTables runs the SQL to set up all tables if they don't exist
func CreateTables() {
	schema := `
	-- Users table (students + admins)
	CREATE TABLE IF NOT EXISTS users (
		id            SERIAL PRIMARY KEY,
		matric_number VARCHAR(20) UNIQUE NOT NULL,
		full_name     VARCHAR(100) NOT NULL,
		email         VARCHAR(100) UNIQUE NOT NULL,
		department    VARCHAR(100) NOT NULL,
		password_hash TEXT NOT NULL,
		role          VARCHAR(10) NOT NULL DEFAULT 'voter',
		is_eligible   BOOLEAN NOT NULL DEFAULT TRUE,
		has_voted     BOOLEAN NOT NULL DEFAULT FALSE,
		created_at    TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Elections table
	CREATE TABLE IF NOT EXISTS elections (
		id          SERIAL PRIMARY KEY,
		title       VARCHAR(200) NOT NULL,
		description TEXT,
		start_time  TIMESTAMP NOT NULL,
		end_time    TIMESTAMP NOT NULL,
		is_active   BOOLEAN NOT NULL DEFAULT FALSE,
		created_at  TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Candidates table
	CREATE TABLE IF NOT EXISTS candidates (
		id          SERIAL PRIMARY KEY,
		election_id INT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
		full_name   VARCHAR(100) NOT NULL,
		position    VARCHAR(100) NOT NULL,
		department  VARCHAR(100),
		manifesto   TEXT,
		vote_count  INT NOT NULL DEFAULT 0
	);

	-- Votes table — the actual ballot ledger
	-- The UNIQUE constraint on (user_id, election_id) enforces ONE STUDENT ONE VOTE
	CREATE TABLE IF NOT EXISTS votes (
		id           SERIAL PRIMARY KEY,
		user_id      INT NOT NULL REFERENCES users(id),
		election_id  INT NOT NULL REFERENCES elections(id),
		candidate_id INT NOT NULL REFERENCES candidates(id),
		vote_receipt VARCHAR(64) NOT NULL UNIQUE,
		cast_at      TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, election_id)   -- This is the "one student, one vote" enforcement
	);

	-- Audit logs for transparency
	CREATE TABLE IF NOT EXISTS audit_logs (
		id         SERIAL PRIMARY KEY,
		user_id    INT REFERENCES users(id),
		action     VARCHAR(100) NOT NULL,
		details    TEXT,
		ip_address VARCHAR(50),
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);
	`

	_, err := DB.Exec(schema)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	fmt.Println("✅ Database tables ready")
}
