package main

import (
	"log"

	"github.com/antithesishq/antithesis-sdk-go/lifecycle"
)

func main() {
	log.Println("[setup-complete] signaling setup complete to Antithesis")
	lifecycle.SetupComplete(map[string]any{
		"message": "system is healthy, ready for fault injection",
	})
	log.Println("[setup-complete] done")
}
