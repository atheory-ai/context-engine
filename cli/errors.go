package cli

import "fmt"

func printFirstRunGuide() {
	fmt.Print(`
Context Engine — First Run

CE hasn't been set up yet. To get started:

  1. Navigate to your project directory:
       cd /path/to/your/project

  2. Initialize Context Engine:
       ce project init

  3. Index your codebase:
       ce index

  4. Start querying:
       ce query "how does X work?"

For more information: https://docs.atheory.ai/ce/quickstart

`)
}
