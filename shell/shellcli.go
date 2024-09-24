package shell

import (
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/anti-raid/spintrack/strutils"
	"github.com/go-andiamo/splitter"
	"github.com/peterh/liner"
)

// ShellCli is a simple shell-like interface with commands
type ShellCli[T any] struct {
	ProjectName      string
	Commands         map[string]*Command[T]
	Splitter         splitter.Splitter
	ArgSplitter      splitter.Splitter
	CaseInsensitive  bool
	DebugCompletions bool
	Prompter         func(*ShellCli[T]) string
	Data             *T
	HistoryPath      string

	line *liner.State
}

// Returns a help command
func (s *ShellCli[T]) Help() *Command[T] {
	return &Command[T]{
		Name:        "help",
		Description: "Get help for a command",
		Args: [][3]string{
			{"command", "Command to get help for", ""},
		},
		Completer: func(a *ShellCli[T], line string, args map[string]string) ([]string, error) {
			cmd, ok := args["command"]
			if !ok || cmd == "" {
				cmds := make([]string, 0, len(a.Commands))

				for name := range a.Commands {
					cmds = append(cmds, name)
				}
				return cmds, nil
			}

			cmd = strings.ToLower(cmd)

			var completions []string

			for name := range a.Commands {
				if strings.HasPrefix(name, cmd) {
					completions = append(completions, name)
				}
			}

			return completions, nil
		},
		Run: func(a *ShellCli[T], args map[string]string) error {
			if arg, ok := args["command"]; ok && arg != "" {
				cmd, ok := a.Commands[arg]

				if !ok {
					return fmt.Errorf("unknown command: %s", arg)
				}

				fmt.Println("Command: ", arg)
				fmt.Println("Description: ", cmd.Description)
				fmt.Println("Arguments: ")

				for _, cmd := range cmd.Args {
					fmt.Print("  ", cmd[0], " : ", cmd[1], " (default: ", cmd[2], ")\n")
				}
			} else {
				fmt.Println("Commands: ")

				for cmd, desc := range a.Commands {
					fmt.Print("  ", cmd, ": ", desc.Description, "\n")
				}

				fmt.Println("Use 'help <command>' to get help for a specific command")
			}

			return nil
		},
	}
}

// Command is a command for the shell client
type Command[T any] struct {
	Name        string // Name of the command to use by default
	Description string
	Args        [][3]string // Map of argument to the description and default value
	Run         func(a *ShellCli[T], args map[string]string) error
	Completer   func(a *ShellCli[T], line string, args map[string]string) ([]string, error)
}

// Init initializes the shell client
func (a *ShellCli[T]) Init() error {
	encs := []*splitter.Enclosure{
		splitter.Parenthesis, splitter.SquareBrackets, splitter.CurlyBrackets,
		splitter.DoubleQuotesBackSlashEscaped, splitter.SingleQuotesBackSlashEscaped,
	}

	var err error
	a.Splitter, err = splitter.NewSplitter(' ', encs...)

	if err != nil {
		return fmt.Errorf("error initializing tokenizer: %s", err)
	}

	a.Splitter.AddDefaultOptions(splitter.IgnoreEmptyFirst, splitter.IgnoreEmptyLast, splitter.TrimSpaces)

	a.ArgSplitter, err = splitter.NewSplitter('=', encs...)

	if err != nil {
		return fmt.Errorf("error initializing arg tokenizer: %s", err)
	}

	a.ArgSplitter.AddDefaultOptions(splitter.IgnoreEmptyFirst, splitter.IgnoreEmptyLast, splitter.TrimSpaces, splitter.UnescapeQuotes)

	a.HistoryPath = path.Join(os.TempDir(), a.HistoryPath)

	return nil
}

func (a *ShellCli[T]) ParseOutCommand(cmd []string) (*Command[T], error) {
	if len(cmd) == 0 {
		return nil, nil
	}

	cmdName := cmd[0]

	if a.CaseInsensitive {
		cmdName = strings.ToLower(cmdName)
	}

	cmdData, ok := a.Commands[cmdName]

	if !ok {
		return nil, fmt.Errorf("unknown command: %s", cmd[0])
	}

	return cmdData, nil
}

func (a *ShellCli[T]) CreateArgMapFromArgs(cmdData *Command[T], args []string) (map[string]string, error) {
	argMap := make(map[string]string)

	for i, arg := range args {
		fields, err := a.ArgSplitter.Split(arg)

		if err != nil {
			return nil, fmt.Errorf("error splitting argument: %s", err)
		}

		if len(fields) == 1 {
			if len(cmdData.Args) <= i {
				fmt.Println("WARNING: extra argument: ", fields[0])
				continue
			}

			argMap[cmdData.Args[i][0]] = fields[0]

			continue
		}

		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid argument: %s", arg)
		}

		argMap[fields[0]] = fields[1]
	}

	return argMap, nil
}

// Exec executes a command
func (a *ShellCli[T]) Exec(cmd []string) error {
	cmdData, err := a.ParseOutCommand(cmd)

	if err != nil {
		return err
	}

	if cmdData == nil {
		return nil
	}

	args := cmd[1:]

	argMap, err := a.CreateArgMapFromArgs(cmdData, args)

	if err != nil {
		return err
	}

	err = cmdData.Run(a, argMap)

	if err != nil {
		return err
	}

	return nil
}

func (a *ShellCli[T]) RunString(command string) (bool, error) {
	command = strings.TrimSpace(command)

	tokens, err := a.Splitter.Split(command)

	if err != nil {
		return false, fmt.Errorf("error splitting command: %s", err)
	}

	if len(tokens) == 0 || tokens[0] == "" {
		return false, nil
	}

	if tokens[0] == "exit" || tokens[0] == "quit" {
		return true, nil
	}

	if a.line != nil {
		a.line.AppendHistory(command)
	}

	err = a.Exec(tokens)

	if err != nil {
		return false, err
	}

	return false, nil
}

// AddCommand adds a command to the shell client
//
// It is recommended to use this to add a command over directly modifying the Commands map
// as this function will be updated to be backwards compatible with future changes
func (a *ShellCli[T]) AddCommand(name string, cmd *Command[T]) {
	if a.Commands == nil {
		a.Commands = make(map[string]*Command[T])
	}

	a.Commands[name] = cmd
}

// ExecuteCommands handles a list of commands in the form 'cmd; cmd etc.'
func (a *ShellCli[T]) ExecuteCommands(cmd string) (cancel bool, err error) {
	for _, c := range strings.Split(cmd, ";") {
		if c == "" {
			continue
		}

		cancel, err := a.RunString(c)

		if err != nil || cancel {
			return cancel, err
		}
	}

	return false, nil
}

// Run constantly prompts for input and os.Exit()'s on interrupt signal
//
// Only use this for actual shell apps
func (a *ShellCli[T]) Run() {
	err := a.Init()

	if err != nil {
		fmt.Println("Error initializing cli: ", err)
		os.Exit(1)
	}

	a.line = liner.NewLiner()
	defer a.line.Close()
	OnInterrupt(func() {
		a.line.Close()
	})

	a.line.SetCtrlCAborts(true)
	a.line.SetTabCompletionStyle(liner.TabPrints)

	a.line.SetCompleter(a.CompletionHandler) // Set the completion handler
	a.loadHistory()

	defer a.saveHistory()

	for {
		cmd, err := a.line.Prompt(a.Prompter(a))
		if err != nil {
			if err != io.EOF {
				fmt.Printf("Prompt Error: %v\n", err)
			}
			return
		}

		cancel, err := a.ExecuteCommands(cmd)

		if err != nil {
			fmt.Println("Error: ", err)
		}

		if cancel {
			return
		}
	}
}

// CompletionHandler is the completion handler for the shell client
//
// This may be useful for bash completion scripts etc.
//
// A simple getcompletion command is also provided by the builtin GetCompletion() command
func (a *ShellCli[T]) CompletionHandler(line string) (c []string) {
	// If empty, show all commands
	if len(strings.ReplaceAll(line, " ", "")) == 0 {
		for name := range a.Commands {
			if strings.HasPrefix(name, strings.ToLower(line)) {
				c = append(c, name)
			}
		}
		return c
	} else {
		if strings.Contains(line, ";") {
			return // Don't try to complete commands with semicolons for now
		}

		command := strings.TrimSpace(line)

		tokens, err := a.Splitter.Split(command)

		if err != nil {
			if a.DebugCompletions {
				fmt.Println(err)
			}
			return
		}

		if len(tokens) == 0 || tokens[0] == "" {
			return
		}

		if tokens[0] == "exit" || tokens[0] == "quit" {
			return
		}

		// Try calling the command's completer
		cmdData, err := a.ParseOutCommand(tokens)

		if err != nil {
			if a.DebugCompletions {
				fmt.Println("error parsing command: ", err)
			}

			for name := range a.Commands {
				if strings.HasPrefix(name, strings.ToLower(line)) {
					c = append(c, name)
				}
			}
			return
		}

		if cmdData == nil {
			return
		}

		// If the command has a completer, run it
		if cmdData.Completer != nil {
			args := tokens[1:]

			argMap, err := a.CreateArgMapFromArgs(cmdData, args)

			if err != nil {
				if a.DebugCompletions {
					fmt.Println("error creating arg map: ", err)
				}
				return
			}

			completions, err := cmdData.Completer(a, line, argMap)

			if err != nil {
				if a.DebugCompletions {
					fmt.Println("error running completer: ", err)
				}
				return
			}

			c = completions

			// Add a space to the end of each option
			for i, completion := range c {
				c[i] = completion + " "
			}

			return
		}

		// If the command has no completer, return nothing
		return []string{}
	}
}

// ArgBasedCompletionHandler is a completion handler that can be used as a fallback
func ArgBasedCompletionHandler[T any](a *ShellCli[T], cmd *Command[T], line string, args map[string]string) (c []string, err error) {
	// Case 1: In the middle of typing out an argument
	argsStr := strings.Replace(strings.TrimSpace(line), cmd.Name, "", 1)

	// Check if the user is at an '=' sign. This means that we should not provide completions at all as they want to type out a value
	lastArg := UtilFindLastArgInArgStr(argsStr)

	if strings.HasSuffix(lastArg, "=") {
		return
	}

	// Look for an untyped arg, args are in format a=b
	untypedArg := UtilFindUntypedArgInArgStr(argsStr)

	if a.DebugCompletions {
		fmt.Println("Untyped arg:", untypedArg, "Last arg:", lastArg, "Args:", argsStr)
	}

	if untypedArg != "" {
		// Case #1: There is an untyped arg
		for _, i := range cmd.Args {
			if strings.HasPrefix(i[0], untypedArg) {
				c = append(c, strings.TrimSpace(strutils.ReplaceFromBack(line, untypedArg, "", 1))+" "+i[0]+"=")
			}
		}
	} else {
		// Case #2: List all args the user does not have
		for _, i := range cmd.Args {
			if _, ok := args[i[0]]; !ok {
				c = append(c, cmd.Name+" "+i[0]+"=")
			}
		}
	}

	return
}

// Returns a get completion command
func (s *ShellCli[T]) GetCompletion() *Command[T] {
	var cmd *Command[T]
	cmd = &Command[T]{
		Name:        "getcompletion",
		Description: "Get help for a command",
		Args: [][3]string{
			{"line", "line to get completion for. Use @empty for empty line", ""},
			{"format", "format to return completions in (printNewlineArray/printArray/strJoinArray_spaceSep/strJoinArray_newlineSep/strJoinArray_commaSep/strJoinArray_commaSpaceSep)", "printNewlineArray"},
		},
		Completer: func(a *ShellCli[T], line string, args map[string]string) ([]string, error) {
			return ArgBasedCompletionHandler(a, cmd, line, args)
		},
		Run: func(a *ShellCli[T], args map[string]string) error {
			line, ok := args["line"]
			if !ok || line == "" {
				return fmt.Errorf("no line provided")
			}

			if line == "@empty" {
				line = ""
			}

			format, ok := args["format"]

			if !ok || format == "" {
				format = "printNewlineArray"
			}

			completions := a.CompletionHandler(line)

			switch format {
			case "printNewlineArray":
				for i, completion := range completions {
					fmt.Println(strconv.Itoa(i) + ") " + completion)
				}
			case "printArray":
				fmt.Println(completions)
			case "strJoinArray_spaceSep":
				fmt.Println(strings.Join(completions, " "))
			case "strJoinArray_newlineSep":
				fmt.Println(strings.Join(completions, "\n"))
			case "strJoinArray_commaSep":
				fmt.Println(strings.Join(completions, ","))
			case "strJoinArray_commaSpaceSep":
				fmt.Println(strings.Join(completions, ", "))
			default:
				return fmt.Errorf("unknown format: %s", format)
			}

			return nil
		},
	}

	return cmd
}

func (a *ShellCli[T]) loadHistory() {
	if f, err := os.Open(a.HistoryPath); err == nil {
		a.line.ReadHistory(f)
		f.Close()
	}
}

func (a *ShellCli[T]) saveHistory() {
	if f, err := os.Create(a.HistoryPath); err != nil {
		fmt.Printf("Error creating history file: %v\n", err)
	} else {
		if _, err = a.line.WriteHistory(f); err != nil {
			fmt.Printf("Error writing history file: %v\n", err)
		}
		f.Close()
	}
}

// Look for an untyped arg, args are in format a=b, a='b', a="b". We need to find the last arg that is not closed
//
// # Test cases
//
// abc=def ghi=d ghi:json='{\"ahshsh\":\"ahs=293d\"==w=1 fhfhf}' should return nil as there is no untyped arg
//
// abc=def ghi should return ghi
//
// abc=def ghi= should return nil as ghi is typed due to the =
func UtilFindUntypedArgInArgStr(args string) string {
	// Split the input by spaces, preserving quoted arguments
	var splitArgs []string
	var currentArg strings.Builder
	inQuotes := false

	for _, char := range args {
		if char == '\'' || char == '"' {
			inQuotes = !inQuotes // Toggle inQuotes state
		}
		if char == ' ' && !inQuotes {
			if currentArg.Len() > 0 {
				splitArgs = append(splitArgs, currentArg.String())
				currentArg.Reset()
			}
		} else {
			currentArg.WriteRune(char)
		}
	}

	// Append the last argument if there's any
	if currentArg.Len() > 0 {
		splitArgs = append(splitArgs, currentArg.String())
	}

	// Initialize a variable to store the last untyped argument
	var lastUntyped string

	for _, arg := range splitArgs {
		// Check if the argument has an equals sign and is not closed
		if !strings.Contains(arg, "=") {
			lastUntyped = arg // Update the last untyped argument
		} else if strings.HasSuffix(arg, "=") {
			lastUntyped = "" // If an argument ends with '=', reset lastUntyped
		}
	}

	return lastUntyped
}

// Finds the argument pertaining to the last '=' in the string
func UtilFindLastArgInArgStr(args string) string {
	// Split the input by spaces, preserving quoted arguments
	var splitArgs []string
	var currentArg strings.Builder
	inQuotes := false

	for _, char := range args {
		if char == '\'' || char == '"' {
			inQuotes = !inQuotes // Toggle inQuotes state
		}
		if char == ' ' && !inQuotes {
			if currentArg.Len() > 0 {
				splitArgs = append(splitArgs, currentArg.String())
				currentArg.Reset()
			}
		} else {
			currentArg.WriteRune(char)
		}
	}

	// Append the last argument if there's any
	if currentArg.Len() > 0 {
		splitArgs = append(splitArgs, currentArg.String())
	}

	// Initialize a variable to store the last argument
	var lastArg string

	for _, arg := range splitArgs {
		// Check if the argument has an equals sign
		if strings.Contains(arg, "=") {
			lastArg = arg // Update the last argument
		}
	}

	return lastArg
}
