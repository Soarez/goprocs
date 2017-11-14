package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
)

type cmdlineOrFalse string

func (cmdline cmdlineOrFalse) MarshalJSON() ([]byte, error) {
	if len(cmdline) > 0 {
		return json.Marshal(string(cmdline))
	} else {
		return json.Marshal(false)
	}
}

type ProcInfo struct {
	Pid     int
	Ppid    int
	Name    string
	Cmdline cmdlineOrFalse
	Environ map[string]string
}

const procPath = "/proc"

func readDir(dir string) ([]os.FileInfo, error) {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func readCmdline(pid int) string {
	cmdFile := path.Join(procPath, strconv.Itoa(pid), "cmdline")
	cmdlineBytes, err := ioutil.ReadFile(cmdFile)
	if err != nil {
		panic(fmt.Sprintf("Failed to read cmdline file: %v", err))
	}
	cmdline := strings.Replace(string(cmdlineBytes), "\000", " ", -1)
	if len(cmdline) > 0 {
		cmdline = cmdline[:len(cmdline)-1]
	}
	return cmdline
}

func readEnviron(pid int) map[string]string {
	environFile := path.Join(procPath, strconv.Itoa(pid), "environ")
	var environ map[string]string
	if environBytes, err := ioutil.ReadFile(environFile); err == nil {
		environEntries := strings.Split(string(environBytes), "\000")
		environ = make(map[string]string)
		for _, environEntry := range environEntries {
			entryParts := strings.Split(environEntry, "=")
			name := entryParts[0]
			if len(name) == 0 {
				continue
			}
			var value string
			if len(entryParts) > 1 {
				value = entryParts[1]
			}
			environ[name] = value
		}
	}

	return environ
}

func readProc(pid int) *ProcInfo {
	statFile := path.Join(procPath, strconv.Itoa(pid), "stat")
	statBytes, err := ioutil.ReadFile(statFile)
	if err != nil {
		panic(fmt.Sprintf("Failed to read stat file: %v", err))
	}
	stat := strings.Split(string(statBytes), " ")
	name := stat[1]
	if len(name) > 0 {
		name = name[1 : len(name)-1]
	}
	ppid, err := strconv.Atoi(stat[3])
	if err != nil {
		panic(fmt.Sprintf("Failed to parse ppid as int: %v", err))
	}
	cmdline := readCmdline(pid)
	environ := readEnviron(pid)

	return &ProcInfo{pid, ppid, name, cmdlineOrFalse(cmdline), environ}
}

func readAllProcs() []*ProcInfo {
	entries, err := readDir(procPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to read procfs: %v", err))
	}

	ownPid := os.Getpid()
	procs := make([]*ProcInfo, 0)
	for _, f := range entries {
		pid, err := strconv.Atoi(f.Name())
		if err != nil || pid == ownPid {
			continue
		}
		procs = append(procs, readProc(pid))
	}
	return procs
}

type serverHandler struct{}

func (s *serverHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var status = http.StatusOK
	var body []byte
	if jsonBytes, err := json.Marshal(readAllProcs()); err == nil {
		body = jsonBytes
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		status = http.StatusInternalServerError
		body = []byte(fmt.Sprintf("Failed to marshall: %v", err))
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%v", len(body)))
	w.WriteHeader(status)
	w.Write(body)
}

func main() {
	addr := ":" + os.Getenv("PORT")
	if addr == ":" {
		addr = ":8888"
	}
	httpServer := &http.Server{Addr: addr, Handler: &serverHandler{}}

	go func() {
		fmt.Printf("Listening on http://0.0.0.0%s\n", addr)
		if err := httpServer.ListenAndServe(); err != nil {
			panic(err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	httpServer.Shutdown(context.Background())
}
