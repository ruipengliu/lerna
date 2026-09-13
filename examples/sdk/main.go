// This consumer uses a contract fixture, not a real Harness task service.
package main

import (
	"context"
	"encoding/json"
	"lerna/contractfixture"
	"lerna/schema"
	"lerna/sdk"
	"log"
	"os"
)

func main() {
	registry, err := schema.New([]schema.Resource{contractfixture.SampleResource()})
	if err != nil {
		log.Fatal(err)
	}
	client := sdk.NewClient(contractfixture.New(registry), registry)
	response, err := client.Submit(context.Background(), sdk.Submission{
		Namespace: "example", MessageID: "message-1", OperationID: "operation-1", Goal: "Validate the SDK contract sample",
		Input: contractfixture.SamplePayload(`{"count":1,"large":"9007199254740993"}`),
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
		log.Fatal(err)
	}
}
