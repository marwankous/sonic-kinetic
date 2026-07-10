import { Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { HttpClient } from '@angular/common/http';

export interface TimelineSegment {
  timestamp: string;
  duration_sec: number;
  phase_name: string;
  energy_level: string;
  cue_script: string;
  audio_base64?: string;
}

export interface ComposeResponse {
  title: string;
  pacing_curve: number[];
  timeline: TimelineSegment[];
}

interface ComposeRequest {
  title: string;
  total_length_mins: number;
  focus_areas: string[];
  intensity: string;
}

/** Maps a heart-rate curve onto an SVG viewBox of the given size. Pure so it's independently testable. */
export function pacingCurveToPoints(curve: number[], width = 300, height = 100): string {
  if (curve.length === 0) return '';
  if (curve.length === 1) return `0,${(height / 2).toFixed(1)} ${width},${(height / 2).toFixed(1)}`;

  const min = Math.min(...curve);
  const max = Math.max(...curve);
  const range = max - min || 1;
  const stepX = width / (curve.length - 1);

  return curve
    .map((v, i) => {
      const x = stepX * i;
      const y = height - ((v - min) / range) * height;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
}

const API_URL = 'http://localhost:8080/api/compose';

@Component({
  selector: 'app-workout-composer',
  standalone: true,
  imports: [FormsModule],
  templateUrl: './workout-composer.html',
  styleUrl: './workout-composer.css',
})
export class WorkoutComposerComponent {
  private http = inject(HttpClient);

  callsign = 'RECON-01';
  lengthMins = 20;
  focusAreasInput = 'full body, core, cardio';
  intensity = 'Progressive Overload';
  readonly intensityOptions = ['Steady State', 'Progressive Overload', 'HIIT Shock', 'Pyramid'];

  loading = signal(false);
  error = signal<string | null>(null);
  plan = signal<ComposeResponse | null>(null);

  activeIndex = signal(-1);
  remainingSec = signal(0);
  running = signal(false);

  polylinePoints = computed(() => pacingCurveToPoints(this.plan()?.pacing_curve ?? []));

  private timerHandle: ReturnType<typeof setInterval> | null = null;
  private audioEl: HTMLAudioElement | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => this.stop());
  }

  compose(): void {
    this.stop();
    this.error.set(null);
    this.loading.set(true);
    this.plan.set(null);

    const req: ComposeRequest = {
      title: this.callsign,
      total_length_mins: Number(this.lengthMins),
      focus_areas: this.focusAreasInput.split(',').map((s) => s.trim()).filter(Boolean),
      intensity: this.intensity,
    };

    this.http.post<ComposeResponse>(API_URL, req).subscribe({
      next: (res) => {
        this.plan.set(res);
        this.loading.set(false);
      },
      error: (err) => {
        this.error.set(err?.error?.error ?? 'TRANSMISSION FAILED — backend offline?');
        this.loading.set(false);
      },
    });
  }

  start(): void {
    const plan = this.plan();
    if (!plan || plan.timeline.length === 0 || this.running()) return;
    this.running.set(true);
    this.playSegment(0);
  }

  stop(): void {
    this.running.set(false);
    this.activeIndex.set(-1);
    if (this.timerHandle) {
      clearInterval(this.timerHandle);
      this.timerHandle = null;
    }
    if (this.audioEl) {
      this.audioEl.pause();
      this.audioEl = null;
    }
  }

  private playSegment(index: number): void {
    const plan = this.plan();
    if (!plan || !this.running()) return;
    if (index >= plan.timeline.length) {
      this.stop();
      return;
    }

    const segment = plan.timeline[index];
    this.activeIndex.set(index);
    this.remainingSec.set(segment.duration_sec);

    if (this.audioEl) this.audioEl.pause();
    this.audioEl = segment.audio_base64 ? new Audio(`data:audio/mpeg;base64,${segment.audio_base64}`) : null;
    this.audioEl?.play().catch(() => {});

    if (this.timerHandle) clearInterval(this.timerHandle);
    this.timerHandle = setInterval(() => {
      const remaining = this.remainingSec() - 1;
      if (remaining <= 0) {
        if (this.timerHandle) clearInterval(this.timerHandle);
        this.playSegment(index + 1);
      } else {
        this.remainingSec.set(remaining);
      }
    }, 1000);
  }
}
