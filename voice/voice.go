package voice

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
	"github.com/faiface/beep/speaker"
	"github.com/garlicgarrison/elevenlabs2/client"
	"github.com/garlicgarrison/elevenlabs2/client/types"
	"github.com/garlicgarrison/go-recorder/recorder"
	"github.com/garlicgarrison/go-recorder/wavseg"
	openai "github.com/sashabaranov/go-openai"
)

const (
	DefaultSampleRateStream beep.SampleRate = 44100
)

type Voice struct {
	openAI *openai.Client
	eleven *client.Client
	kill   chan bool
	r      *recorder.Recorder
}

func NewVoice(client *openai.Client, eleven *client.Client, r *recorder.Recorder) *Voice {
	return &Voice{
		openAI: client,
		eleven: eleven,
		kill:   make(chan bool),
		r:      r,
	}
}

func (v *Voice) Start() {
	err := speaker.Init(DefaultSampleRateStream, DefaultSampleRateStream.N(time.Second/10))
	if err != nil {
		return
	}
	prompt := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: `You are a fun loving Spanish friend named Rafaela. 
				We are having a conversation and you are going to teach me words by conversation. 
				Do not act like a teacher but make me learn by having a conversation with me.
				You may speak English when I do not understand something.`,
		},
		{
			Role: openai.ChatMessageRoleSystem,
			Content: `Today's words are persona, tiempo, cosa, casa, trabajo, nuevo, bueno. Integrate
						these words seamlessly into the conversation. Make the conversation fun by 
						not asking too many questions, telling occasional jokes or funny stories. 
						Keep it concise when necessary.`,
		},
	}

	go func() {
		for {
			buffer, err := v.r.RecordVAD(recorder.WAV)
			if err != nil {
				log.Fatalf("recording error -- %s", err)
			}

			segments := wavseg.WavSeg(buffer)
			if segments == nil {
				return
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

				err = os.Rename(file.Name(), file.Name()+".wav")
				if err != nil {
					log.Fatalf("temp file error -- %s", err)
				}

				_, err = file.Write(b.Bytes())
				if err != nil {
					log.Fatalf("file write error -- %s", err)
				}

				wg.Add(1)
				go func(id int, f *os.File) {
					defer func() {
						err = os.Remove(f.Name() + ".wav")
						if err != nil {
							log.Printf("could not remove file error -- %s", err)
						}
						wg.Done()
					}()

					res, err := v.openAI.CreateTranscription(ctx, openai.AudioRequest{
						Model:       openai.Whisper1,
						FilePath:    file.Name() + ".wav",
						Prompt:      "I will speak English and Spanish",
						Temperature: 0.5,
					})
					if err != nil {
						errChan <- err
						return
					}

					transcriptions[id] = res.Text
				}(id, file)
			}
			wg.Wait()
			close(errChan)

			for e := range errChan {
				if e != nil {
					log.Printf("openAI error -- %s", e)
				}
			}

			input := strings.Join(transcriptions, " ")
			if input == "" {
				return
			}
			log.Printf("input -- %s", input)

			// send input to gpt
			prompt = append(prompt, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleUser,
				Content: input,
			})
			completion, err := v.openAI.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
				Model:    openai.GPT3Dot5Turbo,
				Messages: prompt,
			})
			if err != nil {
				log.Printf("openAI error -- %s", err)
				return
			}
			log.Printf("completion -- %s", completion.Choices[0].Message.Content)

			err = v.playTTS(ctx, completion.Choices[0].Message.Content)
			if err != nil {
				log.Printf("tts error -- %s", err)
				return
			}

			prompt = append(prompt, openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: completion.Choices[0].Message.Content,
			})
		}
	}()

	<-v.kill
}

func (v *Voice) Close() {
	v.kill <- true
}

func (v *Voice) playTTS(ctx context.Context, text string) error {
	// get TTS
	voices, err := v.eleven.GetVoiceIDs(ctx)
	if err != nil {
		return err
	}

	tts, err := v.eleven.TTS(ctx, "eleven_multilingual_v1", text, voices[0], types.SynthesisOptions{})
	if err != nil {
		return err
	}

	reader := bytes.NewReader(tts)
	readCloser := io.NopCloser(reader)
	defer readCloser.Close()

	streamer, _, err := mp3.Decode(readCloser)
	if err != nil {
		return err
	}
	defer streamer.Close()

	// play tts with speaker
	done := make(chan bool)
	speaker.Play(beep.Seq(streamer, beep.Callback(func() {
		done <- true
	})))
	defer speaker.Clear()

	<-done
	return nil
}
