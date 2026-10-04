# Target v1 frozen writer sources

Exact published ticket04 source pin `cac8e9225b0394b5156bf3f83ad2f11b2b709855` (merged f11135b; root checkpoint270ae4a). `target.go.txt`, `writer_linux.go.txt` and `0001_target.sql` are byte-for-byte `git show` outputs, with SHA256SUMS. No current v2 target implementation is imported by this writer.

The upgrade test copies these verified sources to its owned overlay scope, changes only package `target` to `main`, and replaces the embed declaration with the exact frozen SQL string. A tiny driver main invokes this real v1 Open/Write/replay/Settings/Observer/Close and emits its own facts. This keeps the root's sole go.mod and fixed sqlite driver, without a nested module or invented SQL layout setup. The frozen writer must actually exit before the v2 reader/writer opens that same file. It is a normal historical-writer fixture, not SIGKILL evidence.
