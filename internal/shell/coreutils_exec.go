//go:build !solaris && !illumos

package shell

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"mvdan.cc/sh/moreinterp/coreutils"
	"mvdan.cc/sh/v3/interp"
)

// coreUtilsExecHandler installs mvdan.cc/sh's Go coreutils. It is only wired
// in when useGoCoreUtils is set. On illumos and solaris, coreutils_exec_stub.go
// supplies a nil handler because the coreutils package does not build there.
var coreUtilsExecHandler execMiddleware = func(next interp.ExecHandlerFunc) interp.ExecHandlerFunc {
	base := coreutils.ExecHandler(next)
	return func(ctx context.Context, args []string) error {
		if len(args) == 0 {
			return next(ctx, args)
		}
		if args[0] != "sleep" {
			return base(ctx, args)
		}
		return coreutilsSleep(ctx, args[1:])
	}
}

// coreutilsSleep fills the upstream coreutils gap without requiring a PATH
// executable on Windows. Fractional intervals and s/m/h/d suffixes are
// accepted, and cancellation interrupts even a very long interval.
func coreutilsSleep(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	invalid := func() error {
		fmt.Fprintln(interp.HandlerCtx(ctx).Stderr, "sleep: expected non-negative finite intervals with optional s/m/h/d suffixes")
		return interp.ExitStatus(1)
	}
	if len(args) == 0 {
		return invalid()
	}
	var total time.Duration
	for _, arg := range args {
		if arg == "" {
			return invalid()
		}
		unit := time.Second
		switch arg[len(arg)-1] {
		case 's':
			arg = arg[:len(arg)-1]
		case 'm':
			arg, unit = arg[:len(arg)-1], time.Minute
		case 'h':
			arg, unit = arg[:len(arg)-1], time.Hour
		case 'd':
			arg, unit = arg[:len(arg)-1], 24*time.Hour
		}
		value, err := strconv.ParseFloat(arg, 64)
		nanos := value * float64(unit)
		if err != nil || value < 0 || math.IsNaN(nanos) || math.IsInf(nanos, 0) || nanos >= float64(math.MaxInt64) {
			return invalid()
		}
		interval := time.Duration(nanos)
		if interval > time.Duration(math.MaxInt64)-total {
			return invalid()
		}
		total += interval
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(total)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
