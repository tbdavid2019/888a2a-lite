package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/sdk/httpclient"
)

type credentialFile struct {
	HubURL   string            `json:"hubUrl"`
	Identity hub.AgentIdentity `json:"identity"`
}

func runCLI(command string, args []string) error {
	switch command {
	case "register":
		return runRegister(args)
	case "peers":
		return runPeers(args)
	case "notify":
		return runNotify(args)
	case "inbox":
		return runInbox(args)
	case "ack":
		return runAck(args)
	default:
		return fmt.Errorf("unknown command %q; supported commands: server, register, peers, notify, inbox, ack", command)
	}
}

func runRegister(args []string) error {
	flags := flag.NewFlagSet("register", flag.ContinueOnError)
	hubURL := flags.String("hub", "", "Hub URL")
	credentialPath := flags.String("credential-file", "", "0600 credential file path")
	declaration := hub.AgentDeclaration{}
	flags.StringVar(&declaration.DisplayName, "name", "", "Agent display name")
	flags.StringVar(&declaration.ProviderFamily, "provider", "", "provider family")
	flags.StringVar(&declaration.TransportID, "transport", "http-json", "transport ID")
	flags.StringVar(&declaration.RegistrationIdempotency, "registration-key", "", "stable installation key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *hubURL == "" || *credentialPath == "" {
		return errors.New("--hub and --credential-file are required")
	}
	client, err := httpclient.New(*hubURL)
	if err != nil {
		return err
	}
	response, err := client.Register(context.Background(), declaration)
	if err != nil {
		return err
	}
	if response.Identity.AgentToken == "" {
		return errors.New("registration did not issue a token; use the existing credential file")
	}
	if err := saveCredentials(*credentialPath, credentialFile{HubURL: *hubURL, Identity: response.Identity}); err != nil {
		return err
	}
	return writeOutput(map[string]any{
		"hubId": response.Identity.HubID, "agentId": response.Identity.AgentID,
		"expiresAt": response.Identity.ExpiresAt, "credentialFile": *credentialPath,
	})
}

func runPeers(args []string) error {
	client, err := clientFromCredentials(args, "peers")
	if err != nil {
		return err
	}
	agents, err := client.ListPeers(context.Background())
	if err != nil {
		return err
	}
	return writeOutput(map[string]any{"agents": agents})
}

func runNotify(args []string) error {
	flags := flag.NewFlagSet("notify", flag.ContinueOnError)
	credentialPath := flags.String("credential-file", "", "credential file path")
	target := flags.String("to", "", "target Agent ID")
	contextID := flags.String("context-id", "", "context ID")
	idempotencyKey := flags.String("idempotency-key", "", "idempotency key")
	taskID := flags.String("task-id", "", "task ID")
	message := flags.String("message", "", "message")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *target == "" || *contextID == "" || *idempotencyKey == "" || *taskID == "" || *message == "" {
		return errors.New("--to, --context-id, --idempotency-key, --task-id, and --message are required")
	}
	client, err := loadClient(*credentialPath)
	if err != nil {
		return err
	}
	item, duplicate, err := client.SendTask(context.Background(), hub.TaskDelivery{
		TargetAgentID: *target, ContextID: *contextID, IdempotencyKey: *idempotencyKey,
		TaskID: *taskID, Message: *message,
	})
	if err != nil {
		return err
	}
	return writeOutput(map[string]any{"taskId": item.TaskID, "sequence": item.Sequence, "duplicate": duplicate})
}

func runInbox(args []string) error {
	flags := flag.NewFlagSet("inbox", flag.ContinueOnError)
	credentialPath := flags.String("credential-file", "", "credential file path")
	after := flags.Uint64("after-sequence", 0, "return items after this sequence")
	limit := flags.Int("limit", 100, "maximum number of items")
	if err := flags.Parse(args); err != nil {
		return err
	}
	client, err := loadClient(*credentialPath)
	if err != nil {
		return err
	}
	response, err := client.PollInbox(context.Background(), *after, *limit)
	if err != nil {
		return err
	}
	return writeOutput(response)
}

func runAck(args []string) error {
	flags := flag.NewFlagSet("ack", flag.ContinueOnError)
	credentialPath := flags.String("credential-file", "", "credential file path")
	sequence := flags.Uint64("sequence", 0, "inbox sequence")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *sequence == 0 {
		return errors.New("--sequence is required and must be positive")
	}
	client, err := loadClient(*credentialPath)
	if err != nil {
		return err
	}
	if err := client.Acknowledge(context.Background(), *sequence); err != nil {
		return err
	}
	return writeOutput(map[string]any{"sequence": *sequence, "state": string(hub.DeliveryStateAcknowledged)})
}

func clientFromCredentials(args []string, command string) (*httpclient.Client, error) {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	credentialPath := flags.String("credential-file", "", "credential file path")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	return loadClient(*credentialPath)
}

func loadClient(path string) (*httpclient.Client, error) {
	if path == "" {
		return nil, errors.New("--credential-file is required")
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read credential file: %w", err)
	}
	var credentials credentialFile
	if err := json.Unmarshal(data, &credentials); err != nil {
		return nil, errors.New("credential file is invalid")
	}
	client, err := httpclient.New(credentials.HubURL)
	if err != nil {
		return nil, err
	}
	client.AgentID = credentials.Identity.AgentID
	client.AgentToken = credentials.Identity.AgentToken
	return client, nil
}

func saveCredentials(path string, credentials credentialFile) error {
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credential file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(path)), 0700); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create credential file: %w", err)
	}
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return fmt.Errorf("protect credential file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write credential file: %w", err)
	}
	return nil
}

func writeOutput(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
