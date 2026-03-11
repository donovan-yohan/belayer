// internal/cli/mail.go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/mail"
	"github.com/spf13/cobra"
)

func newMailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mail",
		Short: "Read and manage mail messages",
	}

	cmd.AddCommand(newMailReadCmd())
	cmd.AddCommand(newMailInboxCmd())
	cmd.AddCommand(newMailAckCmd())

	return cmd
}

func mailStore(cragName string) (*mail.FileStore, error) {
	name, err := resolveCragName(cragName)
	if err != nil {
		return nil, err
	}

	_, cragDir, err := instance.Load(name)
	if err != nil {
		return nil, err
	}

	mailDir := filepath.Join(cragDir, "mail")
	if err := os.MkdirAll(mailDir, 0755); err != nil {
		return nil, fmt.Errorf("creating mail directory: %w", err)
	}

	return mail.NewFileStore(mailDir), nil
}

func newMailReadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read",
		Short: "Read all unread messages and mark as read",
		RunE: func(cmd *cobra.Command, args []string) error {
			address := os.Getenv("BELAYER_MAIL_ADDRESS")
			if address == "" {
				return fmt.Errorf("BELAYER_MAIL_ADDRESS not set")
			}

			store, err := mailStore("")
			if err != nil {
				return err
			}

			output, err := mail.ReadAndClose(store, address)
			if err != nil {
				return err
			}

			fmt.Print(output)
			return nil
		},
	}
}

func newMailInboxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "List unread messages without marking as read",
		RunE: func(cmd *cobra.Command, args []string) error {
			address := os.Getenv("BELAYER_MAIL_ADDRESS")
			if address == "" {
				return fmt.Errorf("BELAYER_MAIL_ADDRESS not set")
			}

			store, err := mailStore("")
			if err != nil {
				return err
			}

			issues, err := mail.ReadInbox(store, address)
			if err != nil {
				return err
			}

			fmt.Print(mail.FormatMessages(issues))
			return nil
		},
	}
}

func newMailAckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ack <message-id>",
		Short: "Mark a specific message as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := os.Getenv("BELAYER_MAIL_ADDRESS")
			if address == "" {
				return fmt.Errorf("BELAYER_MAIL_ADDRESS not set")
			}

			store, err := mailStore("")
			if err != nil {
				return err
			}

			return store.Close(address, args[0])
		},
	}
}
