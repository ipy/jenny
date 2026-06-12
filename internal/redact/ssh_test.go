package redact

import (
	"strings"
	"testing"
)

func TestRedact_ReplacesSSHPrivateKey(t *testing.T) {
	redactor := NewSecretRedactor(ModeRecover)
	sshKey := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAABFwAAAAdzc2gtcn\n-----END OPENSSH PRIVATE KEY-----"
	input := "SSH key:\n" + sshKey
	result := redactor.Redact(input)

	if !strings.Contains(result, "[REDACTED:") {
		t.Error("Expected SSH private key to be redacted")
	}
	if strings.Contains(result, "OPENSSH PRIVATE KEY") {
		t.Error("Original SSH private key markers should not be present in result")
	}
}

func TestRedact_ReplacesRSAPrivateKey(t *testing.T) {
	redactor := NewSecretRedactor(ModeRecover)
	rsaKey := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0gI9Xw==\n-----END RSA PRIVATE KEY-----"
	input := "RSA key:\n" + rsaKey
	result := redactor.Redact(input)

	if !strings.Contains(result, "[REDACTED:") {
		t.Error("Expected RSA private key to be redacted")
	}
	if strings.Contains(result, "RSA PRIVATE KEY") {
		t.Error("Original RSA private key markers should not be present in result")
	}
}
