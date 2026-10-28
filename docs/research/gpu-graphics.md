---
title: "A GPU device is not a graphics driver tree"
date: 2026-10-28
status: accepted
stage: CURRENT
next: "After implementation, verify the boot-created tree on a GPU host and test Mesa 26.2.5 with that tree present"
tags: [research, gpu, graphics, mesa, nix]
summary: "Separates the missing compiled-path graphics tree from unmeasured Mesa-version behavior and an unresolved DRM ioctl denial."
---

# A GPU device is not a graphics driver tree

**Verified:** 2026-10-28 against `e02bb526bc5b4d59fa5ba7293163cdee44f3ff78`. The owner reproduction
was reported against yolo-jail `0.15.1+390.g29023dea67ea`; no GPU is available in this jail for a fresh
hardware run. The implementation is not present at this revision.

## Findings

A passed-through GPU node does not by itself make the guest graphics stack usable. A graphics driver
may look for libraries and data at fixed paths compiled into it. On NixOS those paths include
`/run/opengl-driver` and, when supplied, `/run/opengl-driver-32`; a package can be present in the Nix
store while those conventional paths are absent. The owner's reproduction found `/dev/dri` but no
`/run/opengl-driver`. Mesa reported that it could not open
`/run/opengl-driver/lib/gbm/dri_gbm.so`, and headless Weston failed to create its EGL surfaceless
backend.

Keep three observations separate:

| Observation | Evidence and limit |
| :--- | :--- |
| Missing compiled-path tree | The reported Mesa loader error names the absent tree. Manually linking Mesa 25.0.7's `lib` and `share` into it let Weston start and identify the AMD GPU. This supports a missing-path cause for that run. |
| Mesa 26.2.5 | The owner saw the image's Mesa move from 25.0.x to 26.2.5 after a rebuild, but did **not** test 26.2.5 with the expected tree present. A separate Mesa regression is neither established nor ruled out. |
| DRM `EACCES` | With the tree hand-created, Xwayland could not create an AMD DRM context (`amdgpu_cs_ctx_create2`, `-13`) although opening the card node read-write succeeded. That locates the reported denial after `open(2)`; the failing ioctl/fd and cause were not captured. It does not prove a device-cgroup or privilege diagnosis. |

The Xwayland result remains an unresolved hardware question. Reproduction needs a real GPU jail and
`strace -f -e trace=ioctl` to identify the denied ioctl and fd. No privilege or security-option remedy
is supported by the current evidence. This jail has no `/dev/dri`, so nested/fixture checks cannot
settle it.

## Current yolo boundary

`gpu.enabled` defaults to false and GPU passthrough is a container launch concern. The current run
path builds device/runtime arguments in [`gpuArgs`](../../internal/cli/run/helpers.go); it does not
provision `/run/opengl-driver`. The host-side GPU checks inspect host devices and driver readiness in
[`sections_devices.go`](../../internal/cli/check/sections_devices.go), not the guest's library tree.

Nix package selection already resolves package entries and their requested outputs in
[`flake.nix`](../../flake.nix), with platform filtering and AMD VA-API's implied Mesa packages in
[`EffectivePackages`](../../internal/config/derived.go). It does not currently publish selected
graphics-driver output paths as boot metadata. A correct tree therefore cannot be derived by scanning
store contents, reverse-matching sonames, or importing the host's `/run` tree.

## Fast-moving — verify before building

The locked nixpkgs revision at this source base is `fe13242281a67641c0102cdb1087f0ebe586c7df`.
Evaluation identifies `mesa` at version `26.2.5` with its `out` output, and `pkgsi686Linux.mesa`
provides the corresponding 32-bit package at `out`. These are current evaluation facts, not stable
supported-version promises; re-evaluate after a lock change. The NVIDIA package attribute is nested
as `linuxPackages.nvidia_x11` (version `595.104.02`), not top-level `nvidia-x11`; its output inventory
includes `lib32`, but its actual graphics `lib`/`share` layout has not been verified. Do not claim
NVIDIA graphics-tree support until the metadata producer validates its real outputs.

The existing package pin surface is documented in [`Packages and Tools`](../../userguide/guides/packages-and-tools.md#nix-packages-image-level):
a package can use a `nixpkgs` revision pin or a source `version`/`url`/`hash` override, which cannot
be combined with nixpkgs pinning. A graphics-specific pointer to that supported discovery is still
needed after the implementation contract is live; this research does not add or change config.

The locked-memory fix is separate. The launch currently clamps the container memlock limit to the
host hard limit (or uses the unlimited literal when the host is already unlimited) in
[`gpuArgs`](../../internal/cli/run/helpers.go). Historical ROCm 7.1.1 reports that an older queue
ring-buffer failure at 8 MB was fixed by newer ROCm userspace, not by a larger cap; current
[`ROCm passthrough guidance`](../reference/rocm-passthrough.md#the-memlock-clamp) explicitly rejects
restoring an unconditional unlimited limit or warning users to raise it. That history is not evidence
about graphics-library lookup or the reported DRM ioctl.

## Sources

- Owner's original reproduction request — firsthand paths, logs, and limits summarized above; not a
  general claim that every Mesa version or graphics application behaves identically.
- [`ROCm passthrough reference`](../reference/rocm-passthrough.md) — current device, memlock, and
  platform contracts; keeps the older memlock hypothesis from being revived.
- [`Image delivery reference`](../reference/image-staging-vs-baking.md) and
  [`flake.nix`](../../flake.nix) — selected package outputs and image/profile delivery boundaries.
