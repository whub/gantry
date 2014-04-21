package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/whub/faucet/cmd"
	"github.com/whub/faucet/fancy"
	"github.com/whub/faucet/sand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Config struct {
	ClientId string `json:"clientId"`
	ApiKey   string `json:"apiKey"`
}

func loadConfig() {
	f, err := os.Open("faucet.json")
	if err != nil {
		fancy.Println(fancy.Red, err)
		os.Exit(1)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	config := Config{}
	err = dec.Decode(&config)
	if err != nil {
		fancy.Println(fancy.Red, err)
		os.Exit(1)
	}
	sand.ClientId = config.ClientId
	sand.ApiKey = config.ApiKey
}

func main() {
	loadConfig()
	root := cmd.Root(os.Args[0])
	root.Command("load", "copy code at a given tag to a machine", "<machine name> <tag>", load)
	root.Command("loaded", "show which code has been copied to a machine", "<machine name>", loaded)
	root.Command("build", "build an image from loaded code on a machine", "<machine name> <docker file> <repo>/<image name>:<tag>", build)
	root.Command("built", "show built images", "<machine name>", built)
	root.Command("up", "run a container on a machine", "<machine name> <repo>/<image name>:<tag> <command>", up)
	root.Command("down", "stop a container on a machine", "<machine name> <container id>", down)
	root.Command("status", "show containers running on a machine", "<machine name>", status)
	err := root.Dispatch(os.Args, 1)
	if err != nil {
		fancy.Println(fancy.Red, err)
	}
}

func status(args []string) error {
	if len(args) != 1 {
		return cmd.ErrInvalidArgs
	}
	name := args[0]
	address, err := dropletAddress(name)
	if err != nil {
		return err
	}
	fmt.Printf("asking %s for container list...\n", name)
	command := exec.Command("ssh", "root@"+address, "docker ps --no-trunc")
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	return nil
}

func load(args []string) error {
	if len(args) != 2 {
		return cmd.ErrInvalidArgs
	}
	name := args[0]
	tag := args[1]
	archiveName, err := getArchive(tag)
	if err != nil {
		return err
	}
	address, err := dropletAddress(name)
	if err != nil {
		return err
	}
	err = rm(archiveName, name, address)
	if err != nil {
		return err
	}
	err = scp(archiveName, name, address)
	if err != nil {
		return err
	}
	return extract(archiveName, name, address)
}

func loaded(args []string) error {
	if len(args) != 1 {
		return cmd.ErrInvalidArgs
	}
	name := args[0]
	address, err := dropletAddress(name)
	if err != nil {
		return err
	}
	fmt.Printf("asking %s to list the home directory... ", name)
	out, err := exec.Command("ssh", "root@"+address, "ls -l").Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	fancy.Println(fancy.Green, "OK")
	return nil
}

func build(args []string) error {
	if len(args) != 3 {
		return cmd.ErrInvalidArgs
	}
	machine := args[0]
	dockerfile := args[1]
	image := args[2]
	address, err := dropletAddress(machine)
	if err != nil {
		return err
	}
	fmt.Printf("building %s on %s... ", image, machine)
	repo := strings.Split(image, "/")[0]
	tag := strings.Split(image, ":")[1]
	folder := fmt.Sprintf("%s-%s", repo, tag)
	cmdStr := fmt.Sprintf("cd %s && cp %s . && docker build -t %s .", folder, dockerfile, image)
	command := exec.Command("ssh", "root@"+address, cmdStr)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	return nil
}

func built(args []string) error {
	if len(args) != 1 {
		return cmd.ErrInvalidArgs
	}
	machine := args[0]
	address, err := dropletAddress(machine)
	if err != nil {
		return err
	}
	fmt.Printf("asking %s for container list... ", machine)
	out, err := exec.Command("ssh", "root@"+address, "docker images").Output()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	fmt.Println(string(out))
	return nil
}

func up(args []string) error {
	if len(args) < 3 {
		return cmd.ErrInvalidArgs
	}
	machine := args[0]
	image := args[1]
	remoteCmd := strings.Join(args[2:], " ") // prolly won't work with quoted args
	address, err := dropletAddress(machine)
	if err != nil {
		return err
	}
	fmt.Printf("asking %s to run container... ", machine)
	out, err := exec.Command("ssh", "root@"+address, fmt.Sprintf("docker run -d -p 443:10443 %s %s", image, remoteCmd)).Output()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	fmt.Println(string(out))
	return nil
}

func down(args []string) error {
	if len(args) != 2 {
		return cmd.ErrInvalidArgs
	}
	name := args[0]
	container := args[1]
	address, err := dropletAddress(name)
	if err != nil {
		return err
	}
	// Run 'docker kill' remotely.
	fmt.Printf("asking %s to stop the container...\n", name)
	command := exec.Command("ssh", "root@"+address, fmt.Sprintf("docker kill %s", container))
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	return nil
}

func dropletAddress(name string) (string, error) {
	fmt.Print("fetching droplet list... ")
	droplets, err := sand.GetDroplets()
	if err != nil {
		return "", err
	}
	fancy.Println(fancy.Green, "OK")
	fmt.Printf("checking for %s... ", name)
	for _, d := range droplets {
		if d.Name == name {
			fancy.Println(fancy.Green, "OK")
			return d.IPAddress, nil
		}
	}
	return "", errors.New(fmt.Sprintf("%s not found", name))
}

func gitInfo() (name, address string, err error) {
	out, err := exec.Command("git", "remote", "-v").Output()
	if err != nil {
		return "", "", err
	}
	tokens := strings.Split(string(out), "\t")
	tokens = strings.Split(tokens[1], " ")
	address = tokens[0]
	tokens = strings.Split(tokens[0], ":")
	name = filepath.Base(tokens[1])
	name = name[:len(name)-len(filepath.Ext(tokens[1]))]
	return name, address, nil
}

func getArchive(tag string) (string, error) {
	fmt.Print("checking for git repo... ")
	repoName, repoAddress, err := gitInfo()
	if err != nil {
		return "", err
	}
	fancy.Println(fancy.Green, "OK")
	name := fmt.Sprintf("/tmp/%s-%s.tar.gz", repoName, tag)
	fmt.Printf("fetching archive of tag %s to %s... ", tag, name)
	command := exec.Command("git", "archive", "-o", name, "--format=tar.gz", "--remote="+repoAddress, tag)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		return "", err
	}
	fancy.Println(fancy.Green, "OK")
	return name, nil
}

func rm(filename, name, address string) error {
	basename := filepath.Base(filename)
	fmt.Printf("removing %s:%s... ", name, basename)
	command := exec.Command("ssh", "root@"+address, "rm -rf "+basename)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	return nil
}

func scp(filename, name, address string) error {
	basename := filepath.Base(filename)
	fmt.Printf("copying %s to %s:%s... ", filename, name, basename)
	command := exec.Command("scp", filename, "root@"+address+":")
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	return nil
}

func extract(filename, name, address string) error {
	basename := filepath.Base(filename)
	folder := basename[:len(basename)-len(".tar.gz")]
	fmt.Printf("extracting %s to %s on %s... ", basename, folder, name)
	err := exec.Command("ssh", "root@"+address, "rm -rf "+folder).Run()
	if err != nil {
		return err
	}
	err = exec.Command("ssh", "root@"+address, "mkdir "+folder).Run()
	if err != nil {
		return err
	}
	err = exec.Command("ssh", "root@"+address, "tar zxvf "+basename+" -C "+folder).Run()
	if err != nil {
		return err
	}
	fancy.Println(fancy.Green, "OK")
	return nil
}
