// Package temporal implements the Temporal workflow and activity definitions
// for belayer v2's Route pipeline.
package temporal

// SignalChannelName is the Temporal signal channel name for CLI callbacks.
// Interactive sessions (Type B roles) send signals via `belayer <role> finish`.
const SignalChannelName = "role-signal"

// TaskQueueName is the default Temporal task queue for belayer workers.
const TaskQueueName = "belayer-route"
