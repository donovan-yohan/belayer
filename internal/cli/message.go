package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/donovan-yohan/belayer/internal/mail"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newMessageCmd() *cobra.Command {
	var (
		bodyFlag    string
		fileFlag    string
		stdinFlag   bool
		typeFlag    string
		subjectFlag string
	)

	cmd := &cobra.Command{
		Use:   "message <address>",
		Short: "Send a mail message to an agent",
		Long:  "Send a typed message to any belayer agent (setter, lead, spotter, anchor). The message is stored on filesystem and delivered via tmux.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			address := args[0]

			// Resolve body from flags (mutually exclusive)
			var body string
			flagCount := 0
			if bodyFlag != "" {
				flagCount++
			}
			if fileFlag != "" {
				flagCount++
			}
			if stdinFlag {
				flagCount++
			}
			if flagCount > 1 {
				return fmt.Errorf("--body, --file, and --stdin are mutually exclusive")
			}
			if flagCount == 0 {
				return fmt.Errorf("one of --body, --file, or --stdin is required")
			}

			switch {
			case bodyFlag != "":
				body = bodyFlag
			case fileFlag != "":
				data, err := os.ReadFile(fileFlag)
				if err != nil {
					return fmt.Errorf("reading file: %w", err)
				}
				body = string(data)
			case stdinFlag:
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
				body = string(data)
			}

			store, err := mailStore("")
			if err != nil {
				return fmt.Errorf("initializing mail store: %w", err)
			}

			from := os.Getenv("BELAYER_MAIL_ADDRESS")

			tm := tmux.NewRealTmux()
			return mail.Send(store, tm, mail.SendOpts{
				To:      address,
				From:    from,
				Type:    mail.MessageType(typeFlag),
				Subject: subjectFlag,
				Body:    body,
			})
		},
	}

	cmd.Flags().StringVar(&bodyFlag, "body", "", "Message body (inline)")
	cmd.Flags().StringVar(&fileFlag, "file", "", "Read body from file")
	cmd.Flags().BoolVar(&stdinFlag, "stdin", false, "Read body from stdin")
	cmd.Flags().StringVar(&typeFlag, "type", "", "Message type (required)")
	cmd.Flags().StringVar(&subjectFlag, "subject", "", "Subject override")
	_ = cmd.MarkFlagRequired("type")

	return cmd
}
