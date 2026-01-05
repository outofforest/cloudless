const std = @import("std");
const builtin = @import("builtin");

pub fn build(b: *std.Build) void {
    const exe = b.addExecutable(.{
        .name = "bootx64",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/main.zig"),
            .optimize = b.standardOptimizeOption(.{}),
            .target = b.resolveTargetQuery(.{
                .cpu_arch = std.Target.Cpu.Arch.x86_64,
                .os_tag = std.Target.Os.Tag.uefi,
                .abi = std.Target.Abi.msvc,
            }),
        }),
    });

    b.installArtifact(exe);
}
