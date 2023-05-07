package voice

import (
	"context"
	"log"
	"os"
	"sync"

	"github.com/garlicgarrison/go-recorder/recorder"
	"github.com/garlicgarrison/go-recorder/vad"
	"github.com/garlicgarrison/go-recorder/wavseg"
	openai "github.com/sashabaranov/go-openai"
)

type Voice struct {
	openAI *openai.Client
	kill   chan bool
	r      *recorder.Recorder
	vad    *vad.VAD
}

func NewVoice(client *openai.Client, r *recorder.Recorder) *Voice {
	// sig := make(chan os.Signal, 1)
	// signal.Notify(sig, os.Interrupt, os.Kill)

	return &Voice{
		openAI: client,
		kill:   make(chan bool),
		r:      r,
	}
}

func (v *Voice) Start() {
	go func() {
		for {
			buffer, err := v.r.RecordVAD(recorder.WAV)
			if err != nil {
				log.Fatalf("recording error -- %s", err)
			}

			segments := wavseg.WavSeg(buffer)
			if segments == nil {
				continue
			}

			ctx := context.Background()
			transcriptions := make([]string, len(segments))
			errChan := make(chan error, len(segments))
			var wg sync.WaitGroup
			for id, b := range segments {
				err := os.Mkdir("tmp", 0777)
				if err != nil && !os.IsExist(err) {
					log.Fatalf("temp dir error -- %s", err)
				}

				file, err := os.CreateTemp("tmp", "transcribe_")
				if err != nil {
					log.Fatalf("temp file error -- %s", err)
				}
				defer os.Remove(file.Name())

				err = os.Rename(file.Name(), file.Name()+".wav")
				if err != nil {
					log.Fatalf("temp file error -- %s", err)
				}

				log.Printf("file name -- %s", file.Name())

				_, err = file.Write(b.Bytes())
				if err != nil {
					log.Fatalf("file write error -- %s", err)
				}

				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					res, err := v.openAI.CreateTranscription(ctx, openai.AudioRequest{
						Model:       openai.Whisper1,
						FilePath:    file.Name() + ".wav",
						Temperature: 0.5,
					})
					log.Printf("res -- %v", res)
					if err != nil {
						errChan <- err
						return
					}

					transcriptions[id] = res.Text
				}(id)
			}
			wg.Wait()
			close(errChan)

			for e := range errChan {
				if e != nil {
					log.Printf("openAI error -- %s", e)
				}
			}
		}
	}()

	<-v.kill
}

func (v *Voice) Close() {
	v.kill <- true
}
