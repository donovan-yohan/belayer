package pipeline

// DefaultPipelineYAML is the built-in setter → lead → spotter pipeline.
const DefaultPipelineYAML = `name: default-climb
nodes:
  - name: setter
    description: |
      You are the setter. You receive a design document and create a detailed
      implementation plan. Do NOT write code. Your output is a plan.md file
      that a separate agent will use to implement the feature.

      Run /harness:plan to create the implementation plan.
      When done, write the plan to .belayer/output/plan.md.
    input:
      type: file
      key: design_doc
    output:
      type: file
      path: .belayer/output/plan.md
    on_pass: next
    on_retry: setter
    on_fail: stop
    max_retries: 2

  - name: lead
    description: |
      You are the lead. You receive an implementation plan and write the code.
      Focus on clean, tested implementation. Follow the plan closely.

      Run /harness:orchestrate to execute the plan.
      Commit your changes to the current branch.
    input:
      type: file
      key: setter
    output:
      type: code
    on_pass: next
    on_retry: self
    on_fail: stop
    max_retries: 3

  - name: spotter
    description: |
      You are the spotter — an adversarial code reviewer. You receive a git
      diff and review it for quality, correctness, and adherence to the plan.

      Run /harness:review to review the changes.
      Write your verdict to .belayer/output/review.md.
      Format: start with PASS, RETRY, or FAIL on the first line.
      If RETRY, include specific feedback for the lead to address.
    input:
      type: code
    output:
      type: file
      path: .belayer/output/review.md
    on_pass: next
    on_retry: lead
    on_fail: stop
    max_retries: 2
`
