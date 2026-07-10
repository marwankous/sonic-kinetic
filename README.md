# Sonic Kinetic

An AI-guided, voice-coached workout generator. Describe a workout — callsign,
length, focus areas, intensity strategy — and it composes a structured
interval timeline with **Gemini**, narrates every cue with **ElevenLabs**
(`eleven_flash_v2_5`), and plays it back as a synced, voice-guided routine in
a dark terminal-style dashboard.

## How it works

1. You submit your workout parameters from the Angular dashboard.
2. The Go backend prompts `gemini-3.1-flash-lite` with a strict JSON schema,
   asking it to break the total duration into sequential 30-90s intervals —
   each with a phase name, energy level, and a spoken coaching line — plus an
   estimated heart-rate pacing curve across the whole routine.
3. Every coaching line is sent concurrently (one goroutine per segment) to
   ElevenLabs for text-to-speech. Segments marked `Maximum` energy get a
   lower `stability` setting (0.30) for a more raw, intense voice.
4. The full timeline, pacing curve, and base64-encoded MP3 audio for every
   segment come back to the client in one response.
5. Hitting **Start Kinetic Routine** walks through the timeline in real
   time — highlighting the active interval, counting its duration down, and
   playing its audio cue the moment it starts.

## Stack

- **Backend**: Go, [Echo](https://echo.labstack.com/) v4, [google.golang.org/genai](https://pkg.go.dev/google.golang.org/genai)
- **Frontend**: Angular (standalone components), vanilla CSS (dark terminal theme)
- **AI**: Gemini (`gemini-3.1-flash-lite`) for structured workout composition, ElevenLabs (`eleven_flash_v2_5`) for TTS

## Running locally

### Backend

```sh
cd backend
cp .env.example .env   # fill in GEMINI_API_KEY and ELEVENLABS_API_KEY
go run .
```

Serves on `http://localhost:8080`.

### Frontend

```sh
cd frontend/client
npm install
npx ng serve
```

Serves on `http://localhost:4200`.

## API

`POST /api/compose`

```json
{
  "title": "RECON-01",
  "total_length_mins": 20,
  "focus_areas": ["core", "cardio"],
  "intensity": "HIIT Shock"
}
```

Returns a `title`, `pacing_curve` (array of estimated heart-rate values), and
a `timeline` array of segments, each with `timestamp`, `duration_sec`,
`phase_name`, `energy_level`, `cue_script`, and `audio_base64`.
