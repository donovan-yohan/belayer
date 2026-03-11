Fetch a tracker issue and create a belayer problem from it.

Usage: /belayer:ticket <ISSUE_ID>

Steps:
1. Run `belayer tracker show <ISSUE_ID>` to preview the issue
2. Ask the user to confirm they want to create a problem from it
3. Run `belayer problem create --ticket <ISSUE_ID> --crag {{.CragName}}`
4. Report the created problem ID
