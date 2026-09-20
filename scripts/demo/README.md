# Demo recording

Scenario for re-recording the README demo GIF (`docs/demo.gif`) with
[asdemokit](https://github.com/asumaran/asdemokit):

```bash
asdemo record            # from the repo root; writes docs/demo.gif
asdemo doctor            # check the toolchain first
```

- `scenario.sh` — the isolated herdr session (`asgotochangeddemo`). No personal
  checkout has a branch worth showing, so `demo_build` clones
  `~/Developer/shopnest` into `~/.cache/asgotochanged-demo/shopnest` and scripts
  a `feat/cart-tax` branch on it: a committed change, an added and a deleted
  file, a pending edit and an untracked note. The real repository is never
  touched, and `demo_teardown` removes the clone. `ASGOTOCHANGED_OPENER` is
  `nvim --clean`, so the editor shows without the user's plugins.
  `demo_setup` renders nothing ahead: the first popup shows hunk's highlight
  coming in, unless the disk cache already holds those patches.
- `keys.json` — `prefix+m` -> popup -> down, down -> type `tax` -> enter (the
  editor) -> `:q` (back to the list) -> `ctrl+t` (diff mode) -> esc.

Known issue: in the recording the popup stays blank while the editor runs.
The GIF is not embedded in the README until that is sorted out.

Besides the kit's toolchain, this needs the `asumaran.asgotochanged` plugin
registered with the `prefix+m` `plugin_action` keybind, `nvim`, `hunk` and
`~/Developer/shopnest` to exist.
