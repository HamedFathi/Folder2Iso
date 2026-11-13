#!/usr/bin/env python3
"""Independent extraction check. Test-only dependency: system libudfread.
Usage: python3 tests/udf_roundtrip.py IMAGE SOURCE_DIRECTORY
Checks every original path, type, length, and byte against the UDF namespace.
"""
import ctypes as C
import ctypes.util
import os
import sys
from pathlib import Path
lib = C.CDLL(ctypes.util.find_library('udfread') or 'libudfread.so.0')
class Entry(C.Structure):
    _fields_ = [('type', C.c_uint), ('name', C.c_char_p)]
def bind(name, result, args):
    f = getattr(lib, name); f.restype = result; f.argtypes = args; return f
ptr = C.c_void_p
init = bind('udfread_init', ptr, [])
open_image = bind('udfread_open', C.c_int, [ptr, C.c_char_p])
close = bind('udfread_close', None, [ptr])
open_dir = bind('udfread_opendir', ptr, [ptr, C.c_char_p])
read_dir = bind('udfread_readdir', C.POINTER(Entry), [ptr, C.POINTER(Entry)])
close_dir = bind('udfread_closedir', None, [ptr])
open_file = bind('udfread_file_open', ptr, [ptr, C.c_char_p])
size_file = bind('udfread_file_size', C.c_int64, [ptr])
read_file = bind('udfread_file_read', C.c_ssize_t, [ptr, ptr, C.c_size_t])
seek_file = bind('udfread_file_seek', C.c_int64, [ptr, C.c_int64, C.c_int])
close_file = bind('udfread_file_close', None, [ptr])
volume = init(); assert volume
try:
    assert open_image(volume, os.fsencode(sys.argv[1])) == 0, 'UDF open failed'
    source = Path(sys.argv[2]); count = 0; buf = C.create_string_buffer(4*1024*1024)
    for root, dirs, files in os.walk(source):
        relative = Path(root).relative_to(source).as_posix()
        relative = '' if relative == '.' else relative
        dh = open_dir(volume, ('/'+relative).encode()); assert dh, relative
        seen = {}; e = Entry()
        try:
            while read_dir(dh, C.byref(e)):
                name = e.name.decode()
                if name not in ('.', '..'): seen[name] = e.type
        finally: close_dir(dh)
        expected = {**{n: 1 for n in dirs}, **{n: 2 for n in files}}
        assert seen == expected, (relative, seen, expected)
        for name in files:
            path = (relative+'/'+name).encode(); fh = open_file(volume, path); assert fh, path
            try:
                original = Path(root)/name
                assert size_file(fh) == original.stat().st_size, path
                with original.open('rb') as f:
                    if '--sample-only' in sys.argv:
                        length = original.stat().st_size
                        # Both sides of every UDF allocation boundary, including AED links.
                        offsets = {0, max(0, length-64)}
                        offsets.update(max(0, x-32) for x in range(0, length, 0x3ffff800))
                        for offset in sorted(offsets):
                            f.seek(offset); wanted = f.read(64)
                            assert seek_file(fh, offset, 0) == offset
                            got = read_file(fh, buf, 64)
                            assert got == len(wanted) and buf.raw[:got] == wanted, (path, offset)
                    else:
                        while True:
                            wanted = f.read(len(buf)); got = read_file(fh, buf, len(buf))
                            assert got == len(wanted) and buf.raw[:got] == wanted, path
                            if not wanted: break
                count += 1
            finally: close_file(fh)
    print(f'UDF round trip passed: {count} files; all directories and names match')
finally: close(volume)
