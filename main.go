package main

import (
	"io/ioutil"
	"log"
	"os"
	"os/signal"

	"github.com/garlicgarrison/go-recorder/recorder"
	"github.com/garlicgarrison/go-recorder/stream"
	"github.com/garlicgarrison/go-recorder/vad"
	"github.com/garlicgarrison/mygpt-cli/voice"
	"github.com/sashabaranov/go-openai"
	"gopkg.in/yaml.v2"

	eleven "github.com/garlicgarrison/elevenlabs2/client"
)

func main() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, os.Kill)

	file, err := os.Open("config/openai.yaml")
	if err != nil {
		log.Fatalf("failed to open YAML file: %v", err)
	}
	defer file.Close()

	// Read the file content
	content, err := ioutil.ReadAll(file)
	if err != nil {
		log.Fatalf("failed to read YAML file: %v", err)
	}

	// Unmarshal the YAML content
	var config map[string]string
	if err := yaml.Unmarshal(content, &config); err != nil {
		log.Fatalf("failed to unmarshal YAML file: %v", err)
	}

	// Get the value of the OPENAI_API_KEY key
	openaiAPIKey, ok := config["OPENAI_API_KEY"]
	if !ok {
		log.Fatalf("OPENAI_API_KEY key not found in YAML file")
	}

	client := openai.NewClient(openaiAPIKey)

	stream, err := stream.NewStream(stream.DefaultStreamConfig())
	if err != nil {
		log.Fatalf("stream error -- %s", err)
	}

	rCfg := &recorder.RecorderConfig{
		SampleRate:      22050,
		InputChannels:   1,
		FramesPerBuffer: 64,
		MaxTime:         100000,

		VADConfig: vad.DefaultVADConfig(),
	}
	recorder, err := recorder.NewRecorder(rCfg, stream)
	if err != nil {
		log.Fatalf("recorder error -- %s", err)
	}

	elevenAPIKey, ok := config["ELEVENLABS_API_KEY"]
	if !ok {
		log.Fatalf("ELEVENLABS_API_KEY key not found in YAML file")
	}
	elevenClient := eleven.New(elevenAPIKey)

	vc := voice.NewVoice(client, &elevenClient, recorder)
	go vc.Start()

	select {
	case <-sig:
		vc.Close()
		os.Exit(0)
	}
}
