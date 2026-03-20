package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/donovan-yohan/belayer/internal/v2/pipeline"
)

func newPipelineCmd() *cobra.Command {
	var pipelineFile string

	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Pipeline DSL commands",
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the pipeline topology",
		RunE: func(cmd *cobra.Command, args []string) error {
			route, source, err := findAndParsePipeline(pipelineFile)
			if err != nil {
				return err
			}
			fmt.Printf("Source: %s\n\n", source)
			fmt.Print(pipeline.Visualize(route, nil))
			return nil
		},
	}
	showCmd.Flags().StringVar(&pipelineFile, "pipeline", "", "Pipeline DSL file")

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the pipeline DSL file",
		RunE: func(cmd *cobra.Command, args []string) error {
			route, source, err := findAndParsePipeline(pipelineFile)
			if err != nil {
				return err
			}
			if err := pipeline.ValidateOrError(route); err != nil {
				return err
			}
			fmt.Printf("Pipeline valid: %s (%s)\n", route.Name, source)
			roles := route.AllRoles()
			fmt.Printf("  %d roles across %d phases\n", len(roles), len(route.Phases))
			loops := route.AllLoops()
			if len(loops) > 0 {
				fmt.Printf("  %d loop(s) configured\n", len(loops))
			}
			return nil
		},
	}
	validateCmd.Flags().StringVar(&pipelineFile, "pipeline", "", "Pipeline DSL file")

	cmd.AddCommand(showCmd, validateCmd)
	return cmd
}
