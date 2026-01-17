package main

import (
	"fmt"

	"github.com/Tributary-ai-services/aether-be/internal/models"
	"github.com/Tributary-ai-services/aether-be/internal/validation"
)

func main() {
	fmt.Print("=== Aether Backend Validation & Sanitization Examples ===\n")

	// Example 1: User Creation with Malicious Input
	fmt.Println("1. User Creation with Malicious Input:")
	maliciousUser := models.UserCreateRequest{
		KeycloakID: "not-a-uuid",
		Email:      "<script>alert('xss')</script>@test.com",
		Username:   "user'; DROP TABLE users; --",
		FullName:   "John <script>alert('hack')</script> Doe",
		AvatarURL:  "javascript:alert('xss')",
	}

	// Sanitize and validate
	fmt.Printf("Original: %+v\n", maliciousUser)

	err := validation.Validate(maliciousUser)
	if err != nil {
		if validationErrors, ok := err.(validation.ValidationErrors); ok {
			fmt.Println("Validation Errors:")
			for _, verr := range validationErrors {
				fmt.Printf("  - %s\n", verr.Message)
			}
		}
	}

	// Example 2: String Sanitization
	fmt.Println("\n2. String Sanitization Examples:")

	testStrings := map[string]string{
		"XSS Script":    "<script>alert('xss')</script>Hello World",
		"SQL Injection": "'; DROP TABLE users; SELECT * FROM passwords WHERE 1=1 --",
		"HTML Content":  "<h1>Title</h1><p>Content with <a href='#'>link</a></p>",
		"Filename":      "../../../etc/passwd<script>.txt",
		"Email":         "  USER@EXAMPLE.COM  ",
		"Username":      "user.name_123!@#$%",
		"Description":   "This is a <b>safe</b> description with 'quotes' and \"more quotes\"",
	}

	for testType, input := range testStrings {
		fmt.Printf("%s:\n", testType)
		fmt.Printf("  Input:  %q\n", input)

		switch testType {
		case "XSS Script", "HTML Content":
			output := validation.SanitizeString(input, validation.DefaultSanitizationOptions())
			fmt.Printf("  Output: %q\n", output)
		case "SQL Injection":
			output := validation.StripSQLInjection(input)
			fmt.Printf("  Output: %q\n", output)
		case "Filename":
			output := validation.SanitizeFilename(input)
			fmt.Printf("  Output: %q\n", output)
		case "Email":
			output := validation.SanitizeEmail(input)
			fmt.Printf("  Output: %q\n", output)
		case "Username":
			output := validation.SanitizeUsername(input)
			fmt.Printf("  Output: %q\n", output)
		case "Description":
			output := validation.SanitizeDescription(input)
			fmt.Printf("  Output: %q\n", output)
		}
		fmt.Println()
	}

	// Example 3: Document Upload Validation
	fmt.Println("3. Document Upload Validation:")
	docRequest := models.DocumentCreateRequest{
		Name:        "../../../dangerous<script>file.pdf",
		Description: "Document with <script>alert('xss')</script> malicious content",
		NotebookID:  "not-a-valid-uuid",
		Tags:        []string{"tag1", "<script>", "valid-tag", "way_too_long_tag_that_exceeds_the_maximum_allowed_length"},
	}

	fmt.Printf("Original: %+v\n", docRequest)

	err = validation.Validate(docRequest)
	if err != nil {
		if validationErrors, ok := err.(validation.ValidationErrors); ok {
			fmt.Println("Validation Errors:")
			for _, verr := range validationErrors {
				fmt.Printf("  - %s\n", verr.Message)
			}
		}
	}

	// Example 4: Notebook Creation with Sanitization
	fmt.Println("\n4. Notebook Creation with Sanitization:")
	notebookRequest := models.NotebookCreateRequest{
		Name:        "My Notebook <script>alert('xss')</script>",
		Description: "A notebook description with 'quotes' and \"more quotes\" and <b>HTML</b>",
		Visibility:  "invalid_visibility",
		Tags:        []string{"research", "data-science", "<script>", "ml"},
	}

	fmt.Printf("Original: %+v\n", notebookRequest)

	// Sanitize names and descriptions
	notebookRequest.Name = validation.SanitizeTitle(notebookRequest.Name)
	notebookRequest.Description = validation.SanitizeDescription(notebookRequest.Description)

	// Sanitize tags
	sanitizedTags := make([]string, 0, len(notebookRequest.Tags))
	for _, tag := range notebookRequest.Tags {
		sanitized := validation.SanitizeTag(tag)
		if sanitized != "" {
			sanitizedTags = append(sanitizedTags, sanitized)
		}
	}
	notebookRequest.Tags = sanitizedTags

	fmt.Printf("Sanitized: %+v\n", notebookRequest)

	err = validation.Validate(notebookRequest)
	if err != nil {
		if validationErrors, ok := err.(validation.ValidationErrors); ok {
			fmt.Println("Validation Errors:")
			for _, verr := range validationErrors {
				fmt.Printf("  - %s\n", verr.Message)
			}
		}
	} else {
		fmt.Println("✅ Validation passed after sanitization!")
	}

	// Example 5: Security Features
	fmt.Println("\n5. Security Features Demonstration:")

	securityTests := map[string]string{
		"Path Traversal":     "../../../etc/passwd",
		"Null Bytes":         "file.txt\x00.exe",
		"Control Characters": "file\x01\x02\x03name.txt",
		"Unicode Attacks":    "file\u202Ename.txt",
		"Long Input":         string(make([]byte, 2000)),
	}

	for testName, input := range securityTests {
		fmt.Printf("%s: %q -> %q\n", testName,
			input[:min(len(input), 50)],
			validation.SanitizeString(input, validation.StrictSanitizationOptions())[:min(50, len(validation.SanitizeString(input, validation.StrictSanitizationOptions())))])
	}

	fmt.Println("\n=== Validation & Sanitization System Ready! ===")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
