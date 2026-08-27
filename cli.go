package main

import (
	"fmt"
	"sort"
	"strings"
)

func runCLI(roots []string, runtime *runtimeManager, args []string) error {
	contexts, err := discoverContexts(roots)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		listed := configuredContexts(contexts)
		if len(args) == 2 && args[1] == "--all" {
			listed = contexts
		} else if len(args) != 1 {
			return fmt.Errorf("usage: dev-runner list [--all]")
		}
		for _, ctx := range listed {
			configured := "unconfigured"
			if ctx.ConfigErr != nil {
				configured = "invalid: " + ctx.ConfigErr.Error()
			} else if ctx.ConfigPath != "" {
				configured = ctx.ConfigPath
			}
			fmt.Printf("%-28s %-24s %s\n", contextSelector(ctx), ctx.ID, configured)
		}
		return nil
	case "start", "stop", "status", "env":
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("usage: dev-runner %s <project/worktree|context-id> [service]", args[0])
		}
		ctx, err := selectContext(contexts, args[1])
		if err != nil {
			return err
		}
		if ctx.ConfigErr != nil {
			return ctx.ConfigErr
		}
		if ctx.ConfigPath == "" {
			return fmt.Errorf("%s has no runner plugin", contextSelector(ctx))
		}
		services := ctx.Config.Services
		if args[0] == "env" {
			if len(args) != 2 {
				return fmt.Errorf("usage: dev-runner env <project/worktree|context-id>")
			}
			env := runtime.Environment(ctx)
			keys := make([]string, 0, len(env))
			for key := range env {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value := env[key]
				if sensitiveName(key) {
					value = "••••••"
				}
				fmt.Printf("%s=%s\n", key, value)
			}
			return nil
		}
		if len(args) == 3 {
			service, ok := serviceByName(ctx.Config, args[2])
			if !ok {
				return fmt.Errorf("service %q not found in %s", args[2], contextSelector(ctx))
			}
			services = []Service{service}
		}
		switch args[0] {
		case "start":
			for _, svc := range services {
				status, err := runtime.Start(ctx, svc)
				if err != nil {
					return err
				}
				fmt.Println(status)
			}
		case "stop":
			for i := len(services) - 1; i >= 0; i-- {
				status, err := runtime.Stop(ctx, services[i])
				if err != nil {
					return err
				}
				fmt.Println(status)
			}
		case "status":
			for _, svc := range services {
				fmt.Printf("%-20s %s\n", svc.Name, runtime.Status(ctx, svc))
			}
		}
		return nil
	case "run":
		if len(args) != 3 {
			return fmt.Errorf("usage: dev-runner run <project[/worktree]|context-id> <action>")
		}
		ctx, err := selectContext(contexts, args[1])
		if err != nil {
			return err
		}
		for _, action := range ctx.Config.Actions {
			if strings.EqualFold(action.Name, args[2]) {
				logs, status, err := runtime.RunAction(ctx, action)
				if logs != "" {
					fmt.Print(logs)
				}
				if err != nil {
					return fmt.Errorf("%s: %w", status, err)
				}
				fmt.Println(status)
				return nil
			}
		}
		return fmt.Errorf("action %q not found in %s", args[2], contextSelector(ctx))
	default:
		return fmt.Errorf("unknown command %q; expected list, start, stop, status, env, or run", args[0])
	}
}

func selectContext(contexts []Context, selector string) (Context, error) {
	selector = strings.ToLower(selector)
	var matches []Context
	for _, ctx := range contexts {
		values := []string{ctx.ID, ctx.Project, contextSelector(ctx)}
		for _, value := range values {
			if strings.EqualFold(value, selector) {
				matches = append(matches, ctx)
				break
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return Context{}, fmt.Errorf("no context matches %q", selector)
	}
	names := make([]string, len(matches))
	for i, match := range matches {
		names[i] = contextSelector(match)
	}
	sort.Strings(names)
	return Context{}, fmt.Errorf("%q is ambiguous; use one of: %s", selector, strings.Join(names, ", "))
}

func contextSelector(ctx Context) string {
	return ctx.Project + "/" + ctx.Worktree
}
