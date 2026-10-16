package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AgentName is the tool that hands tasks to subagents.
const AgentName = "agent"

// SubTask is a task for a subagent, and the name it shows under.
type SubTask struct {
	Task string `json:"task"`
	Name string `json:"name"`
}

// Subagents are the subagents an agent may start and look into: its own
// and theirs, never another branch's. The main agent's are all of them.
type Subagents interface {
	// Run starts the tasks at once, each a subagent of its own, and waits
	// for their reports, or with background returns their ids at once.
	Run(ctx context.Context, tasks []SubTask, background bool) (string, error)
	// Send gives a subagent a message: queued while it works, else it goes
	// on from where it was, waited for unless background.
	Send(ctx context.Context, id int, message string, background bool) (string, error)
	// Wait waits for subagents started in the background, and gives their
	// reports.
	Wait(ctx context.Context, ids []int) (string, error)
	// Log is everything a subagent did, as it reads.
	Log(id int) (string, error)
	Status() string
	Stop(id int) (string, error)
	// TakeEnded are notes for the subagents started in the background that
	// ended since the agent last heard; HasEnded tells whether there are.
	TakeEnded() []string
	HasEnded() bool
}

func agentTool() Tool {
	return Tool{
		Name: AgentName,
		Description: "Hand tasks to subagents, each with its own conversation and your tools; you get only their report. " +
			"action run (default): task and name, or tasks [{task,name}] run at once; background returns their ids at once. " +
			"send id message: tell one more, or let it go on. wait ids, log id (all it did; view fields apply), status, stop id.",
		Schema: json.RawMessage(`{"type":"object","properties":{
"action":{"type":"string","enum":["run","send","wait","log","status","stop"]},
"task":{"type":"string"},"name":{"type":"string"},
"tasks":{"type":"array","items":{"type":"object","properties":{"task":{"type":"string"},"name":{"type":"string"}},"required":["task"]}},
"background":{"type":"boolean"},"wait":{"type":"boolean"},"id":{"type":"integer"},"ids":{"type":"array","items":{"type":"integer"}},"message":{"type":"string"}}}`),
		Run:  runAgent,
		View: true,
		Summary: func(args json.RawMessage) string {
			var fields struct {
				Action, Task, Name, Message string
				ID                          int
				Tasks                       []SubTask
			}
			_ = json.Unmarshal(args, &fields)
			switch {
			case len(fields.Tasks) > 0:
				var names []string
				for _, task := range fields.Tasks {
					names = append(names, orName(task))
				}
				return "run " + strings.Join(names, ", ")
			case fields.Action == "" || fields.Action == "run":
				return "run " + orName(SubTask{Task: fields.Task, Name: fields.Name})
			case fields.ID > 0:
				return fmt.Sprintf("%s %d %s", fields.Action, fields.ID, fields.Message)
			}
			return fields.Action
		},
	}
}

// orName is a task's name, or the start of the task without one.
func orName(task SubTask) string {
	if task.Name != "" {
		return task.Name
	}
	return cutAtRune(strings.Join(strings.Fields(task.Task), " "), 40)
}

func runAgent(ctx context.Context, env Env, raw json.RawMessage) (string, error) {
	var args struct {
		Action     string    `json:"action"`
		Task       string    `json:"task"`
		Name       string    `json:"name"`
		Tasks      []SubTask `json:"tasks"`
		Background bool      `json:"background"`
		Wait       *bool     `json:"wait"`
		ID         int       `json:"id"`
		IDs        []int     `json:"ids"`
		Message    string    `json:"message"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return "", err
	}
	// wait: false is said for background as often.
	if args.Wait != nil && !*args.Wait {
		args.Background = true
	}
	agents := env.Agents
	if agents == nil {
		return "", fmt.Errorf("subagents are off")
	}
	needID := func() error {
		if args.ID <= 0 {
			return fmt.Errorf("%s needs the id of a subagent, as status lists them", args.Action)
		}
		return nil
	}
	switch args.Action {
	case "", "run":
		tasks := args.Tasks
		if strings.TrimSpace(args.Task) != "" {
			tasks = append([]SubTask{{Task: args.Task, Name: args.Name}}, tasks...)
		}
		for _, task := range tasks {
			if strings.TrimSpace(task.Task) == "" {
				return "", fmt.Errorf("every task needs its text")
			}
		}
		if len(tasks) == 0 {
			return "", fmt.Errorf(`run needs a task, e.g. {"task":"find where the token is checked and sum up how","name":"auth"}`)
		}
		return agents.Run(ctx, tasks, args.Background)
	case "send":
		if err := needID(); err != nil {
			return "", err
		}
		if strings.TrimSpace(args.Message) == "" {
			return "", fmt.Errorf("send needs a message")
		}
		return agents.Send(ctx, args.ID, args.Message, args.Background)
	case "wait":
		ids := args.IDs
		if args.ID > 0 {
			ids = append([]int{args.ID}, ids...)
		}
		return agents.Wait(ctx, ids)
	case "log":
		if err := needID(); err != nil {
			return "", err
		}
		return agents.Log(args.ID)
	case "status":
		return agents.Status(), nil
	case "stop":
		if err := needID(); err != nil {
			return "", err
		}
		return agents.Stop(args.ID)
	}
	return "", fmt.Errorf("action %q: use run, send, wait, log, status or stop", args.Action)
}
