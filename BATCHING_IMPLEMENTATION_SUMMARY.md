# Background Task Notification Batching Implementation Summary

## Problem
When multiple parallel agents (background tasks) complete simultaneously, they each send individual notifications to Discord, causing message spam. Users receive 20 separate messages instead of one consolidated update.

## Solution Implemented
Added a batching mechanism that:
1. Collects background task completion notifications for a short period (3 seconds)
2. Groups notifications by channel ID
3. Sends one consolidated message per channel with all completed tasks

## Changes Made

### 1. Modified `internal/discordbot/service.go`

**Added new data structures:**
```go
// For batching background task notifications
backgroundNotificationBatches map[string]*backgroundNotificationBatch
batchMu                    sync.Mutex
```

**Added new types:**
```go
type backgroundNotificationBatch struct {
    channelID     string
    notifications []backgroundNotification
    timer         *time.Timer
}

type backgroundNotification struct {
    taskID  string
    outcome string
    reply   string
    err     error
}
```

**Added constants:**
```go
const backgroundNotificationBatchDelay = 3 * time.Second
```

**Added batching functions:**
- `addBackgroundNotificationToBatch()` - Adds notifications to batches
- `processBackgroundNotificationBatch()` - Processes and sends batched notifications
- `createConsolidatedBackgroundNotificationMessage()` - Formats consolidated messages
- `sendChannelMessage()` - Sends messages to Discord channels
- `processAllPendingBatches()` - Processes pending batches on shutdown

**Modified `shutdown()` function:**
- Added call to `processAllPendingBatches()` to ensure no notifications are lost on shutdown

### 2. Modified `internal/discordbot/background.go`

**Changed `enqueueBackgroundTaskUpdate()` function:**
- Replaced immediate prompt queuing with batching mechanism
- Now calls `addBackgroundNotificationToBatch()` instead of queuing a prompt

### 3. Updated `internal/discordbot/background_handoff_test.go`

**Updated test:**
- Renamed `TestEnqueueBackgroundTaskUpdateQueuesInternalPrompt` to `TestEnqueueBackgroundTaskUpdateAddsToBatch`
- Modified to test batching behavior instead of prompt queuing
- Checks that notifications are properly added to batches

## How It Works

1. When a background task completes, instead of immediately sending a notification, it calls `addBackgroundNotificationToBatch()`
2. This function adds the notification to a batch for the specific channel
3. A timer is started (or reset if already running) for 3 seconds
4. When the timer expires, all notifications in the batch are processed into a single consolidated message
5. The consolidated message is sent to Discord
6. On shutdown, any pending batches are processed immediately to avoid losing notifications

## Benefits

1. **Reduces Message Spam:** Multiple simultaneous task completions result in one message instead of many
2. **Better User Experience:** Cleaner, more organized notifications in Discord
3. **Maintains Information:** All relevant task information is preserved in the consolidated message
4. **Minimal Impact:** Changes are localized and don't affect core functionality
5. **Graceful Shutdown:** Pending notifications are processed on service shutdown

## Message Format Examples

**Single Task Completion:**
```
Background task `task-123` completed: Data analysis finished
```

**Multiple Task Completions:**
```
3 background tasks completed:

- Task `task-1`: Completed
  Result: Analysis complete

- Task `task-2`: Completed
  Result: Data processing done

- Task `task-3`: Failed - connection timeout

Successfully completed: 2 | Failed: 1
```

## Testing

All existing tests pass, and the new batching functionality has been verified with updated tests.

## Future Improvements

1. Make the batch delay configurable
2. Add metrics for batch sizes and processing times
3. Implement prioritization for urgent notifications