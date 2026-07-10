import { Component } from '@angular/core';
import { WorkoutComposerComponent } from './workout-composer/workout-composer';

@Component({
  selector: 'app-root',
  imports: [WorkoutComposerComponent],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class App {}
