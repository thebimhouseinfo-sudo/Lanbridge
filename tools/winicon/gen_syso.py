#!/usr/bin/env python3
"""Generate cmd/bridge/rsrc_windows_amd64.syso embedding an .ico as the exe icon.
Usage: python3 tools/winicon/gen_syso.py internal/ui/icons/desktop.ico cmd/bridge/rsrc_windows_amd64.syso
No external tools needed; the Go linker picks the .syso up automatically."""
import struct, sys

ico = open(sys.argv[1], "rb").read()
_, typ, n = struct.unpack_from("<HHH", ico, 0)
assert typ == 1
imgs = []
for i in range(n):
    w, h, c, r, planes, bpp, size, off = struct.unpack_from("<BBBBHHII", ico, 6 + 16 * i)
    imgs.append((w, h, c, r, planes, bpp, ico[off:off + size]))

LANG = 0x0409
# data blobs: RT_ICON ids 1..n, then one RT_GROUP_ICON id 1
blobs = [im[6] for im in imgs]
grp = struct.pack("<HHH", 0, 1, n)
for i, (w, h, c, r, planes, bpp, data) in enumerate(imgs):
    grp += struct.pack("<BBBBHHIH", w, h, c, r, planes, bpp, len(data), i + 1)
blobs.append(grp)

def dir_(entries):  # entries: list of (id, target_offset, is_subdir)
    out = struct.pack("<IIHHHH", 0, 0, 0, 0, 0, len(entries))
    for id_, tgt, sub in entries:
        out += struct.pack("<II", id_, (0x80000000 | tgt) if sub else tgt)
    return out

# compute layout
root_sz = 16 + 8 * 2
icon_dir_sz = 16 + 8 * n
grp_dir_sz = 16 + 8
lang_dir_sz = 16 + 8
off_root = 0
off_icon_dir = root_sz
off_grp_dir = off_icon_dir + icon_dir_sz
off_icon_names = off_grp_dir + grp_dir_sz            # n lang dirs for icons
off_grp_name = off_icon_names + lang_dir_sz * n      # 1 lang dir for group
off_entries = off_grp_name + lang_dir_sz             # data entries (16 bytes each)
off_data = off_entries + 16 * (n + 1)

# each icon/group has: name dir (id -> lang dir) -- we fold: type dir -> id entry -> subdir(lang) -> data entry
# so type dir entries point to "name dirs"; to keep tree 3 levels we use lang dirs as name->lang level.
# Level2 (type): id -> name dir ; Level3 (name): lang -> data entry directly.
# Re-layout accordingly: name dirs are the 'lang_dir_sz' blocks above, with one entry (lang -> data entry).
blob = bytearray()
relocs = []
data_off = []
cur = off_data
for b in blobs:
    pad = (-cur) % 8
    cur += pad
    data_off.append(cur)
    cur += len(b)

res = bytearray()
res += dir_([(3, off_icon_dir, True), (14, off_grp_dir, True)])
res += dir_([(i + 1, off_icon_names + lang_dir_sz * i, True) for i in range(n)])
res += dir_([(1, off_grp_name, True)])
for i in range(n):
    res += dir_([(LANG, off_entries + 16 * i, False)])
res += dir_([(LANG, off_entries + 16 * n, False)])
for i, b in enumerate(blobs):
    relocs.append(len(res))
    res += struct.pack("<IIII", data_off[i], len(b), 0, 0)
for i, b in enumerate(blobs):
    res += b'\0' * ((data_off[i]) - len(res))
    res += b

raw = bytes(res)
raw += b'\0' * ((-len(raw)) % 4)
hdr_sz, sec_sz = 20, 40
ptr_raw = hdr_sz + sec_sz
ptr_reloc = ptr_raw + len(raw)
ptr_sym = ptr_reloc + 10 * len(relocs)
coff = struct.pack("<HHIIIHH", 0x8664, 1, 0, ptr_sym, 1, 0, 0)
sec = struct.pack("<8sIIIIIIHHI", b".rsrc", 0, 0, len(raw), ptr_raw, ptr_reloc, 0, len(relocs), 0, 0xC0300040)
rel = b"".join(struct.pack("<IIH", r + 12 - 12, 0, 3) for r in relocs)  # IMAGE_REL_AMD64_ADDR32NB at OffsetToData
sym = struct.pack("<8sIhHBB", b".rsrc", 0, 1, 0, 3, 0)
strtab = struct.pack("<I", 4)
open(sys.argv[2], "wb").write(coff + sec + raw + rel + sym + strtab)
print("wrote", sys.argv[2], len(imgs), "icon sizes")
