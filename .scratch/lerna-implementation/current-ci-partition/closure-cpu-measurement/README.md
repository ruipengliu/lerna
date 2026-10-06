# Original closure CPU measurement at d0

The original race test ran once with its default CPU profile and passed in 55.32s. All original 0–65 inputs, Get64 and rejection65 completed. This is a diagnostic result; the preceding whole Race failure and its seven unexecuted calls remain unchanged.

One flat40 and one cumulative40 report used the same exact binary and profile. Both exited 0, completed Wait, left no current process group, and closed their owned raw logs. CPU samples total 36.71s. VersionIdentity shows 3.16s cumulative CPU, including Encode at 2.91s; these values overlap and cannot be added. Mixed transaction, policy, retention and closure stacks do not identify database wall time. Wall minus CPU is not database waiting time.

Both reports preserve the build-ID warning. Independent metadata-only comparison confirms the main profile mapping matches the binary's GNU build-ID exactly. The installed Go pprof object reader returns an empty BuildID; its discovery warning does not prevent the explicit binary from being used for local symbolization. This establishes the warning's cause, not complete attribution of every frame.

The original CPU profile writer and Go binary output writer lack independent first-Close ACKs and remain UNKNOWN. Readonly holder Close, native Wait and process absence do not replace those ACKs. The profile, binary and protected historical resources remain at their registered original identities. No retry, third view, source change or database probe was performed.

The archive index binds each byte-exact original to its original absolute path and hash. Binary/profile contents are retained outside Git and referenced by exact identity in the original certificates. A product optimization and final shared qualification have not yet been accepted.
