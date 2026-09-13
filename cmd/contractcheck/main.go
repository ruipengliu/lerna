// Command contractcheck emits a JSON evidence report and a meaningful exit code.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"lerna/conformance"
	"lerna/profiles/answer"
	"lerna/profiles/asynccheck"
	"lerna/profiles/catalogcheck"
	"lerna/profiles/content"
	"lerna/profiles/contextcheck"
	"lerna/profiles/control"
	"lerna/profiles/credentialcheck"
	"lerna/profiles/durabletasks"
	"lerna/profiles/executioncheck"
	"lerna/profiles/grants"
	"lerna/profiles/localauth"
	"lerna/profiles/memorycheck"
	"lerna/profiles/sdkcontract"
	"lerna/profiles/takeover"
	"lerna/profiles/updates"
	"lerna/profiles/worker"
	"os"
)

func main() {
	if len(os.Args) == 4 && os.Args[1] == "personalized-action-crash-probe" {
		if e := catalogcheck.CheckpointPersonalizedActionAt(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "personalized-answer-crash-probe" {
		if e := answer.CheckpointPersonalizedAnswerAt(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "memory-crash-probe" {
		if e := memorycheck.RunProbe(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "lifecycle-crash-probe" {
		if e := credentialcheck.RunLifecycleProbe(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "catalog-probe" {
		if e := catalogcheck.RunProbe(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && (os.Args[1] == "resource-crash-probe" || os.Args[1] == "resource-recover") {
		f := takeover.RunProbe
		if os.Args[1] == "resource-recover" {
			f = takeover.RecoverProbe
		}
		if e := f(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "async-crash-probe" {
		if e := asynccheck.RunProbe(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "execution-crash-probe" {
		if e := executioncheck.RunProbe(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "updates-crash-probe" {
		if e := updates.RunProbe(context.Background(), os.Args[2], os.Args[3]); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "answer-crash-probe" {
		if err := answer.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "content-crash-probe" {
		if err := content.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "grant-crash-probe" {
		if err := grants.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "control-crash-probe" {
		if err := control.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	if len(os.Args) == 4 && os.Args[1] == "worker-crash-probe" {
		if err := worker.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "task-crash-probe" {
		if err := durabletasks.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "auth-crash-probe" {
		if err := localauth.RunProbe(context.Background(), os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	profileID := flag.String("profile", sdkcontract.ID, "named conformance profile")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(2)
	}
	var profile conformance.Profile
	if *profileID == contextcheck.ID {
		executable, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		profile = contextcheck.Profile(executable)
	} else if *profileID == memorycheck.DeletionProfileID {
		profile = memorycheck.DeletionProfile()
	} else if *profileID == memorycheck.ProfileID {
		profile = memorycheck.Profile()
	} else if *profileID == credentialcheck.LifecycleProfileID {
		profile = credentialcheck.LifecycleProfile()
	} else if *profileID == credentialcheck.ProfileID {
		profile = credentialcheck.Profile()
	} else if *profileID == catalogcheck.ActionProfileID {
		profile = catalogcheck.ActionProfile()
	} else if *profileID == sdkcontract.ID {
		var err error
		profile, err = sdkcontract.Profile()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == localauth.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = localauth.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == durabletasks.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = durabletasks.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == worker.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = worker.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == control.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = control.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == catalogcheck.ID {
		exe, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		profile, e = catalogcheck.Profile(exe)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
	} else if *profileID == takeover.ID {
		exe, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		profile, e = takeover.Profile(exe)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
	} else if *profileID == asynccheck.ID {
		exe, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		profile, e = asynccheck.Profile(exe)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
	} else if *profileID == executioncheck.ID {
		exe, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		profile, e = executioncheck.Profile(exe)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
	} else if *profileID == updates.ID {
		executable, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
		profile, e = updates.Profile(executable)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(2)
		}
	} else if *profileID == answer.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = answer.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == content.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = content.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else if *profileID == grants.ID {
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		profile, err = grants.Profile(executable)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else {
		profile = conformance.Profile{ID: *profileID, Version: "unavailable", Cases: []conformance.Case{{ID: "profile-support", Required: true, Evidence: "compatibility", Availability: conformance.Unsupported}}}
	}
	profile.Method = "contractcheck -profile " + *profileID
	report := conformance.Run(context.Background(), profile)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !report.Passed() {
		os.Exit(1)
	}
}
