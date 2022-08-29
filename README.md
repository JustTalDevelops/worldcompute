# worldcompute

live-computation, parsing, and saves of minecraft: bedrock servers using gophertunnel and dragonfly.

![example of worldcompute on the hive](example.png)

## usage

download the latest release and move it to its own folder. run the executable and authenticate with xbox live.

when completed, worldcompute will save your xbox live token in the same folder under `token.tok`. unless you delete the
file, you'll be able to use worldcompute without authenticating again.

after authenticating, you can close worldcompute. you'll notice that a configuration will be generated. edit the
configuration to your liking, and then run worldcompute. the worldcompute proxy will now forward connections to the
target server specified.

worldrenderer will automatically run and render the chunks in cache in real-time.

## commands

- `reset` - reset all downloaded chunks in cache.
- `save` - save all downloaded chunks to a folder.
- `cancel` - terminate a save-in-progress.

## worldrenderer

worldrenderer will automatically move based on the position of the player in game. you can also use the following controls
to manage the renderer when you aren't moving in-game:

- `up` to move the camera up.
- `down` to move the camera down.
- `left` to move the camera to the left.
- `right` to move the camera to the right.
- `scroll up` to scale the rendered world up.
- `scroll down` to scale the rendered world down.

## supported formats

- `v0` (pre-v1.2.13) (legacy, only used by PM3)
- `v1` (post-v1.2.13, only a single layer)
- `v8/v9` (post-v1.2.13, up to 256 layers) (persistent and runtime)
- `v9 (sub-chunk request system)` (post-v1.18.0, up to 256 layers) (persistent and runtime)
