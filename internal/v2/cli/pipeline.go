package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline DSL commands",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "show",
			Short: "Show the pipeline topology",
			RunE: func(cmd *cobra.Command, args []string) error {
				// TODO: Implement pipeline visualization (Task 10)
				fmt.Println("Pipeline visualization not yet implemented.")
				return nil
			},
		},
		&cobra.Command{
			Use:   "validate",
			Short: "Validate the pipeline DSL file",
			RunE: func(cmd *cobra.Command, args []string) error {
				// TODO: Implement pipeline validation (Task 7)
				fmt.Println("Pipeline validation not yet implemented.")
				return nil
			},
		},
	)

	return cmd
}
