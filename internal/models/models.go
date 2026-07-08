package models

import "time"

// User represents a student or admin in the system
type User struct {
	ID           int       `json:"id"`
	MatricNumber string    `json:"matric_number"`
	FullName     string    `json:"full_name"`
	Email        string    `json:"email"`
	Department   string    `json:"department"`
	PasswordHash string    `json:"-"` // never sent to frontend
	Role         string    `json:"role"` // "voter" or "admin"
	IsEligible   bool      `json:"is_eligible"`
	HasVoted     bool      `json:"has_voted"`
	CreatedAt    time.Time `json:"created_at"`
}

// Candidate represents a person contesting in an election position
type Candidate struct {
	ID          int    `json:"id"`
	ElectionID  int    `json:"election_id"`
	FullName    string `json:"full_name"`
	Position    string `json:"position"`
	Department  string `json:"department"`
	Manifesto   string `json:"manifesto"`
	VoteCount   int    `json:"vote_count"`
}

// Election represents a voting event
type Election struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// Vote represents a single cast ballot
type Vote struct {
	ID          int       `json:"id"`
	UserID      int       `json:"user_id"`
	ElectionID  int       `json:"election_id"`
	CandidateID int       `json:"candidate_id"`
	VoteReceipt string    `json:"vote_receipt"` // unique hash for transparency
	CastAt      time.Time `json:"cast_at"`
}

// AuditLog records system activities for transparency
type AuditLog struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Action    string    `json:"action"`
	Details   string    `json:"details"`
	IPAddress string    `json:"ip_address"`
	CreatedAt time.Time `json:"created_at"`
}

// Request/Response structs (what comes in from the frontend)

type RegisterRequest struct {
	MatricNumber string `json:"matric_number"`
	FullName     string `json:"full_name"`
	Email        string `json:"email"`
	Department   string `json:"department"`
	Password     string `json:"password"`
}

type LoginRequest struct {
	MatricNumber string `json:"matric_number"`
	Password     string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

type CastVoteRequest struct {
	ElectionID  int `json:"election_id"`
	CandidateID int `json:"candidate_id"`
}

type CastVoteResponse struct {
	Message     string `json:"message"`
	VoteReceipt string `json:"vote_receipt"`
}

type ElectionResult struct {
	Election   Election    `json:"election"`
	Candidates []Candidate `json:"candidates"`
	TotalVotes int         `json:"total_votes"`
}
