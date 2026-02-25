package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

// tokenRecord is returned by createToken (stub).
type tokenRecord struct {
	ID        string
	Name      string
	Scope     string
	ExpiresAt *int64
}

func newTokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage API tokens",
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new API token",
		RunE:  runTokenCreate,
	}
	createCmd.Flags().String("name", "", "token name")
	createCmd.Flags().String("scope", "read-write",
		"token scope: read | read-write | admin")
	createCmd.Flags().Int("expires-days", 0,
		"days until expiry (0 = no expiry)")

	cmd.AddCommand(
		createCmd,
		&cobra.Command{
			Use:   "list",
			Short: "List all tokens",
			RunE: func(_ *cobra.Command, _ []string) error {
				fmt.Println("Token management not yet implemented in Phase 1.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "revoke <token-id>",
			Short: "Revoke a token",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, _ []string) error {
				fmt.Println("Token management not yet implemented in Phase 1.")
				return nil
			},
		},
	)

	return cmd
}

func runTokenCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	scope, _ := cmd.Flags().GetString("scope")
	days, _ := cmd.Flags().GetInt("expires-days")

	if name == "" {
		name = promptString("Token name > ")
	}

	switch scope {
	case "read", "read-write", "admin":
		// valid
	default:
		return fmt.Errorf("invalid scope %q — must be: read, read-write, admin", scope)
	}

	token, err := createToken(name, scope, days)
	if err != nil {
		return err
	}

	fmt.Printf("\nToken created:\n\n")
	fmt.Printf("  ID:    %s\n", token.ID)
	fmt.Printf("  Name:  %s\n", token.Name)
	fmt.Printf("  Scope: %s\n", token.Scope)
	if token.ExpiresAt != nil {
		fmt.Printf("  Expires: %s\n", time.UnixMilli(*token.ExpiresAt).Format("2006-01-02"))
	}
	fmt.Println()
	fmt.Println("⚠  This token will not be shown again. Store it securely.")
	fmt.Printf("   CE_TOKEN=%s\n", token.ID)

	return nil
}

// createToken is a stub — Phase 2 will write to meta.db.
func createToken(name, scope string, _ int) (*tokenRecord, error) {
	fmt.Println("Token management not yet implemented in Phase 1.")
	return &tokenRecord{
		ID:    "tok_" + name,
		Name:  name,
		Scope: scope,
	}, nil
}
