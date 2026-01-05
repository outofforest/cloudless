const vmlinuz = @embedFile("embed/vmlinuz");
const initramfs = @embedFile("embed/initramfs");

const uefi = @import("std").os.uefi;

var con_out: *uefi.protocol.SimpleTextOutput = undefined;
var boot_services: *uefi.tables.BootServices = undefined;

pub fn main() uefi.Status {
    con_out = uefi.system_table.con_out.?;
    boot_services = uefi.system_table.boot_services.?;

    log("I am cloudless bootloader.");

    var initramfs_handle: ?uefi.Handle = null;
    var ret = boot_services.installProtocolInterfaces(
        @ptrCast(&initramfs_handle),
        .{
            uefi.protocol.DevicePath,
            &initrd_lf2_handle,
            LoadFile2,
            &lf2_protocol,
        },
    );
    if (ret != uefi.Status.Success) {
        return onError(ret, "Installing initrd media handler failed.");
    }

    var vmlinuz_handle: ?uefi.Handle = null;
    ret = boot_services.loadImage(true, uefi.handle, null, @ptrCast(vmlinuz), vmlinuz.len, &vmlinuz_handle);
    if (ret != uefi.Status.Success) {
        return onError(ret, "Loading kernel failed.");
    }

    var loaded_vmlinuz: *uefi.protocol.LoadedImage = undefined;
    ret = boot_services.handleProtocol(vmlinuz_handle.?, &uefi.protocol.LoadedImage.guid, @ptrCast(&loaded_vmlinuz));
    if (ret != uefi.Status.Success) {
        return onError(ret, "Retrieving kernel image info failed.");
    }
    const cmd = [_:0]u16{
        's', 'e', 'l', 'i','n','u','x','=','0', ' ',
        'd','e','f','a','u','l','t','_','h','u','g','e','p','a','g','e','s','z','=','1','G' , ' ',
        'a', 'u', 'd', 'i', 't', '=', '0',
    };
    loaded_vmlinuz.load_options = @constCast(@ptrCast(&cmd[0]));
    loaded_vmlinuz.load_options_size = 2 * (cmd.len + 1);

    return onError(
        boot_services.startImage(vmlinuz_handle.?, null, null),
        "Booting kernel failed.",
    );
}

fn loadFile(
    _: *const void,
    _: ?*const uefi.protocol.DevicePath,
    boot_policy: bool,
    buffer_size: *usize,
    buffer: ?[*]u8,
) callconv(uefi.cc) uefi.Status {
    if (boot_policy) {
        return uefi.Status.Unsupported;
    }

    if (buffer_size.* < initramfs.len) {
        buffer_size.* = initramfs.len;
        return uefi.Status.BufferTooSmall;
    }

    if (buffer == null) {
        return uefi.Status.InvalidParameter;
    }

    var i: u32 = 0;
    while (i < initramfs.len) {
        buffer.?[i] = initramfs[i];
        i += 1;
    }

    return uefi.Status.Success;
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
};

var initrd_lf2_handle = LF2Handler{
    .vendor = .{
        .type = uefi.DevicePath.Type.Media,
        .subtype = uefi.DevicePath.Media.Subtype.Vendor,
        .length = @sizeOf(uefi.DevicePath.Media.VendorDevicePath),
        .guid = initrd_media_guid,
    },
    .end = .{
        .type = uefi.DevicePath.Type.End,
        .subtype = uefi.DevicePath.End.Subtype.EndEntire,
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

fn onError(s: uefi.Status, msg: []const u8) uefi.Status {
    log(msg);
    switch (s) {
        uefi.Status.LoadError => {
            log("LoadError");
        },
        uefi.Status.InvalidParameter => {
            log("InvalidParameter");
        },
        uefi.Status.Unsupported => {
            log("Unsupported");
        },
        uefi.Status.BadBufferSize => {
            log("BadBufferSize");
        },
        uefi.Status.BufferTooSmall => {
            log("BufferTooSmall");
        },
        uefi.Status.NotReady => {
            log("NotReady");
        },
        uefi.Status.DeviceError => {
            log("DeviceError");
        },
        uefi.Status.WriteProtected => {
            log("WriteProtected");
        },
        uefi.Status.OutOfResources => {
            log("OutOfResources");
        },
        uefi.Status.VolumeCorrupted => {
            log("VolumeCorrupted");
        },
        uefi.Status.VolumeFull => {
            log("VolumeFull");
        },
        uefi.Status.NoMedia => {
            log("NoMedia");
        },
        uefi.Status.MediaChanged => {
            log("MediaChanged");
        },
        uefi.Status.NotFound => {
            log("NotFound");
        },
        uefi.Status.AccessDenied => {
            log("AccessDenied");
        },
        uefi.Status.NoResponse => {
            log("NoResponse");
        },
        uefi.Status.NoMapping => {
            log("NoMapping");
        },
        uefi.Status.Timeout => {
            log("Timeout");
        },
        uefi.Status.NotStarted => {
            log("NotStarted");
        },
        uefi.Status.AlreadyStarted => {
            log("AlreadyStarted");
        },
        uefi.Status.Aborted => {
            log("Aborted");
        },
        uefi.Status.IcmpError => {
            log("IcmpError");
        },
        uefi.Status.TftpError => {
            log("TftpError");
        },
        uefi.Status.ProtocolError => {
            log("ProtocolError");
        },
        uefi.Status.IncompatibleVersion => {
            log("IncompatibleVersion");
        },
        uefi.Status.SecurityViolation => {
            log("SecurityViolation");
        },
        uefi.Status.CrcError => {
            log("CrcError");
        },
        uefi.Status.EndOfMedia => {
            log("EndOfMedia");
        },
        uefi.Status.EndOfFile => {
            log("EndOfFile");
        },
        uefi.Status.InvalidLanguage => {
            log("InvalidLanguage");
        },
        uefi.Status.CompromisedData => {
            log("CompromisedData");
        },
        uefi.Status.IpAddressConflict => {
            log("IpAddressConflict");
        },
        uefi.Status.HttpError => {
            log("HttpError");
        },
        uefi.Status.NetworkUnreachable => {
            log("NetworkUnreachable");
        },
        uefi.Status.HostUnreachable => {
            log("HostUnreachable");
        },
        uefi.Status.ProtocolUnreachable => {
            log("ProtocolUnreachable");
        },
        uefi.Status.PortUnreachable => {
            log("PortUnreachable");
        },
        uefi.Status.ConnectionFin => {
            log("ConnectionFin");
        },
        uefi.Status.ConnectionReset => {
            log("ConnectionReset");
        },
        uefi.Status.ConnectionRefused => {
            log("ConnectionRefused");
        },
        uefi.Status.WarnUnknownGlyph => {
            log("WarnUnknownGlyph");
        },
        uefi.Status.WarnDeleteFailure => {
            log("WarnDeleteFailure");
        },
        uefi.Status.WarnWriteFailure => {
            log("WarnWriteFailure");
        },
        uefi.Status.WarnBufferTooSmall => {
            log("WarnBufferTooSmall");
        },
        uefi.Status.WarnStaleData => {
            log("WarnStaleData");
        },
        uefi.Status.WarnFileSystem => {
            log("WarnFileSystem");
        },
        uefi.Status.WarnResetRequired => {
            log("WarnResetRequired");
        },
        else => {
            log("Unknown error");
        },
    }

    _ = boot_services.stall(30 * 1000 * 1000);

    return s;
}

fn log(msg: []const u8) void {
    for (msg) |c| {
        _ = con_out.outputString(&[_:0]u16{c});
    }
    _ = con_out.outputString(&[_:0]u16{ '\r', '\n' });
}
