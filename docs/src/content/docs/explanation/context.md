---
title: Context checkpoints
description: How the working view stays smaller than the session history.
---

`Session.Messages` is the conversation record. `ContextMessages` projects the view sent to the model, so a checkpoint can shorten old material without deleting the original messages.

At the configured token trigger, the deciding model selects material to keep. `Config.Pruner` can supply a separate model for that decision; the main model is the default.

The checkpoint modes change when a checkpoint occurs. `low` triggers at 48,000 estimated tokens, `moderate` at 32,000 and `extreme` at 20,000. Completed runs can be folded into summaries. The original session log remains available on disk.

The checkpoint policy trades prompt size against the cost of recovering details from earlier turns.
