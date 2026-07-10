package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"google.golang.org/genai"
)

// ComposeRequest is the incoming workout-generation request.
type ComposeRequest struct {
	Title           string   `json:"title"`
	TotalLengthMins int      `json:"total_length_mins"`
	FocusAreas      []string `json:"focus_areas"`
	Intensity       string   `json:"intensity"`
}

// TimelineSegment is one interval block of the generated routine.
// AudioBase64 is populated after the ElevenLabs TTS pass; it is absent
// from the schema we hand to Gemini since Gemini never produces audio.
type TimelineSegment struct {
	Timestamp   string `json:"timestamp"`
	DurationSec int    `json:"duration_sec"`
	PhaseName   string `json:"phase_name"`
	EnergyLevel string `json:"energy_level"`
	CueScript   string `json:"cue_script"`
	AudioBase64 string `json:"audio_base64,omitempty"`
}

// ComposeResponse is the structured plan Gemini returns (and what we
// serve back to the client once audio has been attached).
type ComposeResponse struct {
	Title       string            `json:"title"`
	PacingCurve []int             `json:"pacing_curve"`
	Timeline    []TimelineSegment `json:"timeline"`
}

var geminiSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"title": {Type: genai.TypeString},
		"pacing_curve": {
			Type:  genai.TypeArray,
			Items: &genai.Schema{Type: genai.TypeInteger},
		},
		"timeline": {
			Type: genai.TypeArray,
			Items: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"timestamp":    {Type: genai.TypeString},
					"duration_sec": {Type: genai.TypeInteger, Minimum: floatPtr(30), Maximum: floatPtr(90)},
					"phase_name":   {Type: genai.TypeString},
					"energy_level": {Type: genai.TypeString, Enum: []string{"Low", "Moderate", "High", "Maximum"}},
					"cue_script":   {Type: genai.TypeString},
				},
				Required: []string{"timestamp", "duration_sec", "phase_name", "energy_level", "cue_script"},
			},
		},
	},
	Required: []string{"title", "pacing_curve", "timeline"},
}

func floatPtr(f float64) *float64 { return &f }

func main() {
	_ = godotenv.Load()

	geminiKey := os.Getenv("GEMINI_API_KEY")
	elevenKey := os.Getenv("ELEVENLABS_API_KEY")
	if geminiKey == "" || elevenKey == "" {
		log.Fatal("GEMINI_API_KEY and ELEVENLABS_API_KEY must be set (see .env)")
	}

	genaiClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  geminiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		log.Fatalf("failed to create genai client: %v", err)
	}

	e := echo.New()
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))

	h := &handler{genai: genaiClient, elevenLabsKey: elevenKey}
	e.POST("/api/compose", h.compose)

	e.Logger.Fatal(e.Start(":8080"))
}

type handler struct {
	genai         *genai.Client
	elevenLabsKey string
}

func (h *handler) compose(c echo.Context) error {
	var req ComposeRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if req.TotalLengthMins <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "total_length_mins must be positive"})
	}

	ctx := c.Request().Context()

	plan, err := h.generatePlan(ctx, req)
	if err != nil {
		log.Printf("gemini generation failed: %v", err)
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to generate workout plan"})
	}

	h.synthesizeAudio(ctx, plan)

	return c.JSON(http.StatusOK, plan)
}

func (h *handler) generatePlan(ctx context.Context, req ComposeRequest) (*ComposeResponse, error) {
	prompt := fmt.Sprintf(
		"You are an elite athletic trainer composing an audio-guided workout script. "+
			"Workout callsign: %q. Total duration: %d minutes. Focus areas: %v. Intensity strategy: %q. "+
			"Break the ENTIRE duration down into sequential intervals, each lasting between 30 and 90 seconds, "+
			"until the sum of all interval durations covers the full workout length. "+
			"For each interval provide a punchy, high-energy spoken coaching cue (cue_script) a trainer would "+
			"shout mid-workout, matched to that interval's energy_level (Low, Moderate, High, or Maximum). "+
			"Also produce a pacing_curve: an array of estimated heart-rate values (beats per minute, integers) "+
			"with one entry per interval, reflecting the intensity trajectory across the routine.",
		req.Title, req.TotalLengthMins, req.FocusAreas, req.Intensity,
	)

	result, err := h.genai.Models.GenerateContent(ctx, "gemini-3.1-flash-lite",
		[]*genai.Content{{Parts: []*genai.Part{{Text: prompt}}}},
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   geminiSchema,
		},
	)
	if err != nil {
		return nil, err
	}

	var plan ComposeResponse
	if err := json.Unmarshal([]byte(result.Text()), &plan); err != nil {
		return nil, fmt.Errorf("unmarshal gemini response: %w", err)
	}
	return &plan, nil
}

// synthesizeAudio pipelines every cue_script through ElevenLabs TTS concurrently,
// writing the base64 result directly back onto each segment in place.
func (h *handler) synthesizeAudio(ctx context.Context, plan *ComposeResponse) {
	var wg sync.WaitGroup
	for i := range plan.Timeline {
		wg.Add(1)
		go func(seg *TimelineSegment) {
			defer wg.Done()
			audio, err := h.textToSpeech(ctx, seg.CueScript, seg.EnergyLevel)
			if err != nil {
				log.Printf("tts failed for segment %q: %v", seg.PhaseName, err)
				return
			}
			seg.AudioBase64 = base64.StdEncoding.EncodeToString(audio)
		}(&plan.Timeline[i])
	}
	wg.Wait()
}

type elevenLabsRequest struct {
	Text          string                  `json:"text"`
	ModelID       string                  `json:"model_id"`
	VoiceSettings elevenLabsVoiceSettings `json:"voice_settings"`
}

type elevenLabsVoiceSettings struct {
	Stability       float64 `json:"stability"`
	SimilarityBoost float64 `json:"similarity_boost"`
}

const elevenLabsVoiceID = "pNInz6obpgDQGcFmaJgB" // "Adam" — confirmed API-accessible on free tier; override via env if needed

func (h *handler) textToSpeech(ctx context.Context, text, energyLevel string) ([]byte, error) {
	stability := 0.5
	if energyLevel == "Maximum" {
		stability = 0.30
	}

	body, err := json.Marshal(elevenLabsRequest{
		Text:    text,
		ModelID: "eleven_flash_v2_5",
		VoiceSettings: elevenLabsVoiceSettings{
			Stability:       stability,
			SimilarityBoost: 0.75,
		},
	})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", elevenLabsVoiceID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("xi-api-key", h.elevenLabsKey)
	httpReq.Header.Set("Accept", "audio/mpeg")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("elevenlabs returned %d: %s", resp.StatusCode, errBody)
	}

	return io.ReadAll(resp.Body)
}
