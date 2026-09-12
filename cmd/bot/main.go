package main

import (
	"log"

	"github.com/wafflestudio/dicoco/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
