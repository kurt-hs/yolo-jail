package macosuser

// StateDir is the host-owned staging root every macos-user launch stages into,
// /Users/Shared/yolo-staged (StagingRoot), holding the staged yolo and guest binaries (bin/), each
// session's pack tree (packs/), content tree (home-overlay/), context tree (ctx/) and env file
// (env/), and each session's Seatbelt profile (profile-<key>.sb). Exported for the one reader
// outside this package that needs the path itself rather than a path built from it, `yolo stores`,
// which lists what is in it. The root-owned dir older yolo staged into is LegacyStateDir.
func StateDir() string { return stateDir }
