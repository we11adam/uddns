package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/we11adam/uddns/internal/selfupdate"
)

type selfUpdater interface {
	Check(context.Context, string) (selfupdate.Plan, error)
	Apply(
		context.Context,
		selfupdate.Plan,
		selfupdate.ApplyOptions,
	) (selfupdate.Result, error)
	Rollback(context.Context) (selfupdate.Result, error)
}

type commandDependencies struct {
	newSelfUpdater func() (selfUpdater, error)
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		newSelfUpdater: func() (selfUpdater, error) {
			return selfupdate.NewForCurrentExecutable(version)
		},
	}
}

func runSelfUpdateCommand(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	dependencies commandDependencies,
) int {
	flags := flag.NewFlagSet("uddns self-update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	checkOnly := flags.Bool("check", false, "Check for an update without installing it")
	requestedVersion := flags.String("version", "", "Install a specific stable version")
	jsonOutput := flags.Bool("json", false, "Print the result as JSON")
	rollback := flags.Bool("rollback", false, "Restore the previous installed binary")
	allowDevelopment := flags.Bool(
		"allow-dev",
		false,
		"Allow a development build to be replaced",
	)
	allowDowngrade := flags.Bool(
		"allow-downgrade",
		false,
		"Allow installation of an older version",
	)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "uddns self-update: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if *rollback && (*checkOnly || *requestedVersion != "" ||
		*allowDevelopment || *allowDowngrade) {
		fmt.Fprintln(
			stderr,
			"uddns self-update: --rollback cannot be combined with update options",
		)
		return 2
	}
	if *checkOnly && (*allowDevelopment || *allowDowngrade) {
		fmt.Fprintln(
			stderr,
			"uddns self-update: apply-only options cannot be used with --check",
		)
		return 2
	}
	if dependencies.newSelfUpdater == nil {
		fmt.Fprintln(stderr, "uddns self-update: updater is unavailable")
		return 1
	}

	updater, err := dependencies.newSelfUpdater()
	if err != nil {
		return printSelfUpdateError(stderr, err)
	}
	if *rollback {
		result, err := updater.Rollback(ctx)
		if err != nil {
			return printSelfUpdateOperationError(stderr, err)
		}
		return printSelfUpdateResult(stdout, stderr, result, *jsonOutput, true)
	}

	plan, err := updater.Check(ctx, *requestedVersion)
	if err != nil {
		return printSelfUpdateError(stderr, err)
	}
	if *checkOnly {
		return printSelfUpdatePlan(stdout, stderr, plan, *jsonOutput)
	}

	result, err := updater.Apply(ctx, plan, selfupdate.ApplyOptions{
		AllowDevelopment: *allowDevelopment,
		AllowDowngrade:   *allowDowngrade,
	})
	if err != nil {
		return printSelfUpdateOperationError(stderr, err)
	}
	return printSelfUpdateResult(stdout, stderr, result, *jsonOutput, false)
}

func printSelfUpdatePlan(
	stdout io.Writer,
	stderr io.Writer,
	plan selfupdate.Plan,
	jsonOutput bool,
) int {
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(plan); err != nil {
			return printSelfUpdateError(stderr, err)
		}
		return 0
	}

	var message string
	switch plan.Status {
	case selfupdate.StatusDevelopment:
		message = "development build; latest stable version is " + plan.TargetVersion
	case selfupdate.StatusNewerThanTarget:
		message = fmt.Sprintf(
			"installed version %s is newer than target %s",
			plan.CurrentVersion,
			plan.TargetVersion,
		)
	case selfupdate.StatusUpdateAvailable:
		message = fmt.Sprintf(
			"update available: %s -> %s",
			plan.CurrentVersion,
			plan.TargetVersion,
		)
	case selfupdate.StatusUpToDate:
		message = "up to date: " + plan.TargetVersion
	default:
		return printSelfUpdateError(
			stderr,
			fmt.Errorf("unknown update status %q", plan.Status),
		)
	}
	if _, err := fmt.Fprintln(stdout, message); err != nil {
		return printSelfUpdateError(stderr, err)
	}
	return 0
}

func printSelfUpdateResult(
	stdout io.Writer,
	stderr io.Writer,
	result selfupdate.Result,
	jsonOutput bool,
	rollback bool,
) int {
	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return printSelfUpdateError(stderr, err)
		}
		return 0
	}

	if !result.Changed {
		if _, err := fmt.Fprintf(stdout, "already up to date: %s\n", result.ToVersion); err != nil {
			return printSelfUpdateError(stderr, err)
		}
		return 0
	}
	action := "updated"
	if rollback {
		action = "rolled back"
	}
	if _, err := fmt.Fprintf(
		stdout,
		"%s uddns from %s to %s\nbackup: %s\n",
		action,
		result.FromVersion,
		result.ToVersion,
		result.BackupPath,
	); err != nil {
		return printSelfUpdateError(stderr, err)
	}
	return 0
}

func printSelfUpdateError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "uddns self-update: %v\n", err)
	return 1
}

func printSelfUpdateOperationError(stderr io.Writer, err error) int {
	printSelfUpdateError(stderr, err)
	var permissionError *selfupdate.LocalPermissionError
	if errors.As(err, &permissionError) {
		fmt.Fprintln(stderr, "try running the same command with sudo")
	}
	return 1
}
