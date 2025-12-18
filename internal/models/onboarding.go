package models

import "time"

// OnboardingResult represents the result of user onboarding
type OnboardingResult struct {
	UserID            string            `json:"user_id"`
	Success           bool              `json:"success"`
	DefaultNotebookID string            `json:"default_notebook_id,omitempty"`
	DefaultAgentID    string            `json:"default_agent_id,omitempty"`
	SampleDocsCount   int               `json:"sample_docs_count,omitempty"`
	Steps             map[string]bool   `json:"steps"`
	ErrorMessage      string            `json:"error_message,omitempty"`
	StartedAt         time.Time         `json:"started_at"`
	CompletedAt       time.Time         `json:"completed_at"`
	DurationMs        int64             `json:"duration_ms"`
}

// OnboardingStep represents individual onboarding steps
type OnboardingStep string

const (
	StepPersonalSpace   OnboardingStep = "personal_space"
	StepDefaultNotebook OnboardingStep = "default_notebook"
	StepSampleDocuments OnboardingStep = "sample_documents"
	StepDefaultAgent    OnboardingStep = "default_agent"
)

// SampleDocument represents a pre-configured sample document to upload during onboarding
type SampleDocument struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FilePath    string `json:"file_path"`
	MimeType    string `json:"mime_type"`
	Size        int64  `json:"size"`
}

// DefaultSampleDocuments returns the list of sample documents to create during onboarding
func DefaultSampleDocuments() []SampleDocument {
	return []SampleDocument{
		{
			Name:        "Welcome to Aether.pdf",
			Description: "Introduction to the Aether AI platform and its capabilities",
			MimeType:    "application/pdf",
		},
		{
			Name:        "Quick Start Guide.pdf",
			Description: "Step-by-step guide to common workflows in Aether",
			MimeType:    "application/pdf",
		},
		{
			Name:        "AI Agent Best Practices.pdf",
			Description: "Guidelines for creating and using AI agents effectively",
			MimeType:    "application/pdf",
		},
	}
}
