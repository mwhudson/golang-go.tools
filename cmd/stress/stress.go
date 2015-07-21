// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The stress utility is intended for catching of episodic failures.
// It runs a given process in parallel in a loop and collects any failures.
// Usage:
// 	$ stress ./fmt.test -test.run=TestSometing -test.cpu=10
// You can also specify a number of parallel processes with -p flag;
// instruct the utility to not kill hanged processes for gdb attach;
// or specify the failure output you are looking for (if you want to
// ignore some other episodic failures).
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"syscall"
	"time"
)

var (
	flagP       = flag.Int("p", runtime.NumCPU(), "run `N` processes in parallel")
	flagTimeout = flag.Duration("timeout", 10*time.Minute, "timeout each process after `duration`")
	flagKill    = flag.Bool("kill", true, "kill timed out processes if true, otherwise just print pid (to attach with gdb)")
	flagFailure = flag.String("failure", "", "fail only if output matches `regexp`")
)

func main() {
	flag.Parse()
	if *flagP <= 0 || *flagTimeout <= 0 || len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}
	var failureRe *regexp.Regexp
	if *flagFailure != "" {
		var err error
		if failureRe, err = regexp.Compile(*flagFailure); err != nil {
			fmt.Println("bad failure regexp:", err)
			os.Exit(1)
		}
	}
	res := make(chan []byte)
	for i := 0; i < *flagP; i++ {
		go func() {
			for {
				cmd := exec.Command(flag.Args()[0], flag.Args()[1:]...)
				done := make(chan bool)
				if *flagTimeout > 0 {
					go func() {
						select {
						case <-done:
							return
						case <-time.After(*flagTimeout):
						}
						if !*flagKill {
							fmt.Printf("process %v timed out\n", cmd.Process.Pid)
							return
						}
						cmd.Process.Signal(syscall.SIGABRT)
						select {
						case <-done:
							return
						case <-time.After(10 * time.Second):
						}
						cmd.Process.Kill()
					}()
				}
				out, err := cmd.CombinedOutput()
				close(done)
				if err != nil && (failureRe == nil || failureRe.Match(out)) {
					out = append(out, fmt.Sprintf("\n\nERROR: %v\n", err)...)
				} else {
					out = []byte{}
				}
				res <- out
			}
		}()
	}
	runs, fails := 0, 0
	ticker := time.NewTicker(5 * time.Second).C
	for {
		select {
		case out := <-res:
			runs++
			if len(out) == 0 {
				continue
			}
			fails++
			f, err := ioutil.TempFile("", "go-stress")
			if err != nil {
				fmt.Printf("failed to create temp file: %v\n", err)
				os.Exit(1)
			}
			f.Write(out)
			f.Close()
			if len(out) > 2<<10 {
				out = out[:2<<10]
			}
			fmt.Printf("\n%s\n%s\n", f.Name(), out)
		case <-ticker:
			fmt.Printf("%v runs so far, %v failures\n", runs, fails)
		}
	}
}
