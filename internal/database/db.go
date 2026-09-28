package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

// Connect establishes a connection to PostgreSQL using environment variables
func Connect() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// Fallback for local dev — set your values in .env file, NOT here
		log.Fatal("DATABASE_URL environment variable is not set. Please create a .env file.")
	}

	var err error
	DB, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("✅ Database connected successfully")
}

// CreateTables runs the SQL to set up all tables if they don't exist
func CreateTables() {
	schema := `
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

	CREATE TABLE IF NOT EXISTS elections (
		id          SERIAL PRIMARY KEY,
		title       VARCHAR(200) NOT NULL,
		description TEXT,
		start_time  TIMESTAMP NOT NULL,
		end_time    TIMESTAMP NOT NULL,
		is_active   BOOLEAN NOT NULL DEFAULT FALSE,
		created_at  TIMESTAMP NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS candidates (
		id          SERIAL PRIMARY KEY,
		election_id INT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
		full_name   VARCHAR(100) NOT NULL,
		position    VARCHAR(100) NOT NULL,
		department  VARCHAR(100),
		manifesto   TEXT,
		vote_count  INT NOT NULL DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS votes (
		id           SERIAL PRIMARY KEY,
		user_id      INT NOT NULL REFERENCES users(id),
		election_id  INT NOT NULL REFERENCES elections(id),
		candidate_id INT NOT NULL REFERENCES candidates(id),
		vote_receipt VARCHAR(64) NOT NULL UNIQUE,
		cast_at      TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE (user_id, election_id)
	);

	CREATE TABLE IF NOT EXISTS audit_logs (
		id         SERIAL PRIMARY KEY,
		user_id    INT REFERENCES users(id),
		action     VARCHAR(100) NOT NULL,
		details    TEXT,
		ip_address VARCHAR(50),
		created_at TIMESTAMP NOT NULL DEFAULT NOW()
	);

	-- Fingerprint / Face unlock (WebAuthn passkeys).
	-- Only the device's PUBLIC key is stored here. The fingerprint or face scan
	-- itself never leaves the student's phone or laptop and is never sent to
	-- this server, so there is no biometric data in this database to leak.
	CREATE TABLE IF NOT EXISTS webauthn_credentials (
		id              SERIAL PRIMARY KEY,
		user_id         INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		credential_id   TEXT NOT NULL UNIQUE,
		credential_data JSONB NOT NULL,
		device_label    VARCHAR(100) NOT NULL DEFAULT 'This device',
		sign_count      BIGINT NOT NULL DEFAULT 0,
		created_at      TIMESTAMP NOT NULL DEFAULT NOW(),
		last_used_at    TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user
		ON webauthn_credentials(user_id);

	-- Official list of BOUESTI students allowed to register. Uploaded by an
	-- admin. While this table is empty, registration stays open.
	CREATE TABLE IF NOT EXISTS student_roster (
		matric_number VARCHAR(20) PRIMARY KEY,
		full_name     VARCHAR(100) NOT NULL DEFAULT '',
		department    VARCHAR(100) NOT NULL DEFAULT '',
		created_at    TIMESTAMP NOT NULL DEFAULT NOW()
	);
	`

	_, err := DB.Exec(schema)
	if err != nil {
		log.Fatalf("Failed to create tables: %v", err)
	}

	fmt.Println("✅ Database tables ready")
}
