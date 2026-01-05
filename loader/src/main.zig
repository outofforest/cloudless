const vmlinuz = @embedFile("embed/vmlinuz");
const initramfs = @embedFile("embed/initramfs");

const uefi = @import("std").os.uefi;

var con_out: *uefi.protocol.SimpleTextOutput = undefined;
var boot_services: *uefi.tables.BootServices = undefined;

pub fn main() uefi.Status {
    con_out = uefi.system_table.con_out.?;
    boot_services = uefi.system_table.boot_services.?;

    log("I am cloudless bootloader.");

    _ = boot_services.installProtocolInterfaces(
        null,
        .{
            &initrd_lf2_handle,
            &lf2_protocol,
        },
    ) catch |err| {
        return onError(err, "Installing initrd media handler failed.");
    };

    const vmlinuz_handle = boot_services.loadImage(true, uefi.handle,.{
            .buffer = vmlinuz,
        }) catch |err| {
        return onError(err, "Loading kernel failed.");
    };

    const loaded_vmlinuz = boot_services.handleProtocol(uefi.protocol.LoadedImage, vmlinuz_handle) catch |err| {
        return onError(err, "Retrieving kernel image info failed.");
    };
    const cmd = [_:0]u16{
        's', 'e', 'l', 'i','n','u','x','=','0', ' ',
        'd','e','f','a','u','l','t','_','h','u','g','e','p','a','g','e','s','z','=','1','G' , ' ',
        'a', 'u', 'd', 'i', 't', '=', '0',
    };
    loaded_vmlinuz.?.load_options = @constCast(@ptrCast(&cmd[0]));
    loaded_vmlinuz.?.load_options_size = 2 * (cmd.len + 1);

    _ = boot_services.startImage(vmlinuz_handle) catch |err| {
        return onError(err,"Booting kernel failed.");
    };
    return uefi.Status.success;
}

fn loadFile(
    _: *const void,
    _: ?*const uefi.protocol.DevicePath,
    boot_policy: bool,
    buffer_size: *usize,
    buffer: ?[*]u8,
) callconv(uefi.cc) uefi.Status {
    if (boot_policy) {
        return uefi.Status.unsupported;
    }

    if (buffer_size.* < initramfs.len) {
        buffer_size.* = initramfs.len;
        return uefi.Status.buffer_too_small;
    }

    if (buffer == null) {
        return uefi.Status.invalid_parameter;
    }

    var i: u32 = 0;
    while (i < initramfs.len) {
        buffer.?[i] = initramfs[i];
        i += 1;
    }

    return uefi.Status.success;
}

const initrd_media_guid = uefi.Guid{
    .time_low = 0x5568e427,
    .time_mid = 0x68fc,
    .time_high_and_version = 0x4f3d,
    .clock_seq_high_and_reserved = 0xac,
    .clock_seq_low = 0x74,
    .node = .{ 0xca, 0x55, 0x52, 0x31, 0xcc, 0x68 },
};

const LF2Handler = extern struct {
    vendor: uefi.DevicePath.Media.VendorDevicePath,
    end: uefi.DevicePath.End.EndEntireDevicePath,

    pub const guid align(8) = uefi.protocol.DevicePath.guid;
};

var initrd_lf2_handle = LF2Handler{
    .vendor = .{
        .type = uefi.DevicePath.Type.media,
        .subtype = uefi.DevicePath.Media.Subtype.vendor,
        .length = @sizeOf(uefi.DevicePath.Media.VendorDevicePath),
        .guid = initrd_media_guid,
    },
    .end = .{
        .type = uefi.DevicePath.Type.end,
        .subtype = uefi.DevicePath.End.Subtype.end_entire,
        .length = @sizeOf(uefi.DevicePath.End.EndEntireDevicePath),
    },
};

const LoadFile2 = extern struct {
    _load_file: *const fn (*const void, ?*const uefi.protocol.DevicePath, bool, *usize, [*]u8) callconv(uefi.cc) uefi.Status,

    pub const guid align(8) = uefi.Guid{
        .time_low = 0x4006c0c1,
        .time_mid = 0xfcb3,
        .time_high_and_version = 0x403e,
        .clock_seq_high_and_reserved = 0x99,
        .clock_seq_low = 0x6d,
        .node = .{ 0x4a, 0x6c, 0x87, 0x24, 0xe0, 0x6d },
    };
};

const lf2_protocol = LoadFile2{
    ._load_file = &loadFile,
};

fn onError(err: uefi.Error, msg: []const u8) uefi.Status {
    log(msg);
    const status = uefi.Status.fromError(@errorCast(err));
    switch (status) {
        uefi.Status.load_error => {
            log("LoadError");
        },
        uefi.Status.invalid_parameter => {
            log("InvalidParameter");
        },
        uefi.Status.unsupported => {
            log("Unsupported");
        },
        uefi.Status.bad_buffer_size => {
            log("BadBufferSize");
        },
        uefi.Status.buffer_too_small => {
            log("BufferTooSmall");
        },
        uefi.Status.not_ready => {
            log("NotReady");
        },
        uefi.Status.device_error => {
            log("DeviceError");
        },
        uefi.Status.write_protected => {
            log("WriteProtected");
        },
        uefi.Status.out_of_resources => {
            log("OutOfResources");
        },
        uefi.Status.volume_corrupted => {
            log("VolumeCorrupted");
        },
        uefi.Status.volume_full => {
            log("VolumeFull");
        },
        uefi.Status.no_media => {
            log("NoMedia");
        },
        uefi.Status.media_changed => {
            log("MediaChanged");
        },
        uefi.Status.not_found => {
            log("NotFound");
        },
        uefi.Status.access_denied => {
            log("AccessDenied");
        },
        uefi.Status.no_response => {
            log("NoResponse");
        },
        uefi.Status.no_mapping => {
            log("NoMapping");
        },
        uefi.Status.timeout => {
            log("Timeout");
        },
        uefi.Status.not_started => {
            log("NotStarted");
        },
        uefi.Status.already_started => {
            log("AlreadyStarted");
        },
        uefi.Status.aborted => {
            log("Aborted");
        },
        uefi.Status.icmp_error => {
            log("IcmpError");
        },
        uefi.Status.tftp_error => {
            log("TftpError");
        },
        uefi.Status.protocol_error => {
            log("ProtocolError");
        },
        uefi.Status.incompatible_version => {
            log("IncompatibleVersion");
        },
        uefi.Status.security_violation => {
            log("SecurityViolation");
        },
        uefi.Status.crc_error => {
            log("CrcError");
        },
        uefi.Status.end_of_media => {
            log("EndOfMedia");
        },
        uefi.Status.end_of_file => {
            log("EndOfFile");
        },
        uefi.Status.invalid_language => {
            log("InvalidLanguage");
        },
        uefi.Status.compromised_data => {
            log("CompromisedData");
        },
        uefi.Status.ip_address_conflict => {
            log("IpAddressConflict");
        },
        uefi.Status.http_error => {
            log("HttpError");
        },
        uefi.Status.network_unreachable => {
            log("NetworkUnreachable");
        },
        uefi.Status.host_unreachable => {
            log("HostUnreachable");
        },
        uefi.Status.protocol_unreachable => {
            log("ProtocolUnreachable");
        },
        uefi.Status.port_unreachable => {
            log("PortUnreachable");
        },
        uefi.Status.connection_fin => {
            log("ConnectionFin");
        },
        uefi.Status.connection_reset => {
            log("ConnectionReset");
        },
        uefi.Status.connection_refused => {
            log("ConnectionRefused");
        },
        uefi.Status.warn_unknown_glyph => {
            log("WarnUnknownGlyph");
        },
        uefi.Status.warn_delete_failure => {
            log("WarnDeleteFailure");
        },
        uefi.Status.warn_write_failure => {
            log("WarnWriteFailure");
        },
        uefi.Status.warn_buffer_too_small => {
            log("WarnBufferTooSmall");
        },
        uefi.Status.warn_stale_data => {
            log("WarnStaleData");
        },
        uefi.Status.warn_file_system => {
            log("WarnFileSystem");
        },
        uefi.Status.warn_reset_required => {
            log("WarnResetRequired");
        },
        else => {
            log("Unknown error");
        },
    }

    _ = boot_services.stall(30 * 1000 * 1000) catch {};

    return status;
}

fn log(msg: []const u8) void {
    for (msg) |c| {
        _ = con_out.outputString(&[_:0]u16{c}) catch {};
    }
    _ = con_out.outputString(&[_:0]u16{ '\r', '\n' }) catch {};
}
