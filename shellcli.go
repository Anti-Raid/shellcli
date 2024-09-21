package shellcli

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/go-andiamo/splitter"
	"github.com/peterh/liner"
)

// ShellCli is a simple shell-like interface with commands
type ShellCli[T any] struct {
	ProjectName     string
	Commands        map[string]*Command[T]
	Splitter        splitter.Splitter
	ArgSplitter     splitter.Splitter
	CaseInsensitive bool
	Prompter        func(*ShellCli[T]) string
	Data            *T

	line        *liner.State
	historyPath string
}

// Returns a help command
func (s *ShellCli[T]) Help() *Command[T] {
	return &Command[T]{
		Description: "Get help for a command",
		Args: [][3]string{
			{"command", "Command to get help for", ""},
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
	Description string
	Args        [][3]string // Map of argument to the description and default value
	Run         func(a *ShellCli[T], args map[string]string) error
}

// Init initializes the shell client
func (a *ShellCli[T]) Init() error {
	var err error
	a.Splitter, err = splitter.NewSplitter(' ', splitter.DoubleQuotes, splitter.SingleQuotes)

	if err != nil {
		return fmt.Errorf("error initializing tokenizer: %s", err)
	}

	a.Splitter.AddDefaultOptions(splitter.IgnoreEmptyFirst, splitter.IgnoreEmptyLast, splitter.TrimSpaces, splitter.UnescapeQuotes)

	a.ArgSplitter, err = splitter.NewSplitter('=', splitter.DoubleQuotes, splitter.SingleQuotes)

	if err != nil {
		return fmt.Errorf("error initializing arg tokenizer: %s", err)
	}

	a.ArgSplitter.AddDefaultOptions(splitter.IgnoreEmptyFirst, splitter.IgnoreEmptyLast, splitter.TrimSpaces, splitter.UnescapeQuotes)

	a.historyPath = path.Join(os.TempDir(), "weed-shell")

	return nil
}

// Exec executes a command
func (a *ShellCli[T]) Exec(cmd []string) error {
	if len(cmd) == 0 {
		return nil
	}

	cmdName := cmd[0]

	if a.CaseInsensitive {
		cmdName = strings.ToLower(cmdName)
	}

	cmdData, ok := a.Commands[cmdName]

	if !ok {
		return fmt.Errorf("unknown command: %s", cmd[0])
	}

	args := cmd[1:]

	argMap := make(map[string]string)

	for i, arg := range args {
		fields, err := a.ArgSplitter.Split(arg)

		if err != nil {
			return fmt.Errorf("error splitting argument: %s", err)
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
			return fmt.Errorf("invalid argument: %s", arg)
		}

		argMap[fields[0]] = fields[1]
	}

	err := cmdData.Run(a, argMap)

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

	a.setCompletionHandler()
	a.loadHistory()

	defer a.saveHistory()

	for {
		cmd, err := a.line.Prompt(a.Prompter(a))
		if err != nil {
			if err != io.EOF {
				fmt.Printf("%v\n", err)
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

func (a *ShellCli[T]) setCompletionHandler() {
	a.line.SetCompleter(func(line string) (c []string) {
		for name := range a.Commands {
			if strings.HasPrefix(name, strings.ToLower(line)) {
				c = append(c, name)
			}
		}
		return
	})
}

func (a *ShellCli[T]) loadHistory() {
	if f, err := os.Open(a.historyPath); err == nil {
		a.line.ReadHistory(f)
		f.Close()
	}
}

func (a *ShellCli[T]) saveHistory() {
	if f, err := os.Create(a.historyPath); err != nil {
		fmt.Printf("Error creating history file: %v\n", err)
	} else {
		if _, err = a.line.WriteHistory(f); err != nil {
			fmt.Printf("Error writing history file: %v\n", err)
		}
		f.Close()
	}
}
