#!/usr/bin/env python3
"""Generate the high-contrast desktop app icons. Requires Pillow."""
from pathlib import Path
from PIL import Image, ImageDraw

out = Path(__file__).resolve().parents[2] / 'internal' / 'ui' / 'icons'
im = Image.new('RGBA', (1024, 1024), (0, 0, 0, 0))
d = ImageDraw.Draw(im)
d.rounded_rectangle((40, 40, 984, 984), radius=225, fill='#0752A1')
# A continuous cable linking two plugs.
white = '#FFFFFF'
d.line([(275, 410), (275, 645)], fill=white, width=54)
d.arc((275, 505, 515, 745), 0, 180, fill=white, width=54)
d.line([(488, 625), (488, 342)], fill=white, width=54)
d.arc((488, 222, 728, 462), 180, 360, fill=white, width=54)
d.line([(701, 342), (701, 600)], fill=white, width=54)
d.rounded_rectangle((190, 265, 360, 410), radius=35, outline=white, width=40)
d.line([(230, 265), (230, 205)], fill=white, width=40)
d.line([(320, 265), (320, 205)], fill=white, width=40)
d.rounded_rectangle((616, 600, 786, 745), radius=35, outline=white, width=40)
d.line([(656, 745), (656, 805)], fill=white, width=40)
d.line([(746, 745), (746, 805)], fill=white, width=40)
im.save(out / 'AppIcon.icns', format='ICNS')
im.save(out / 'desktop.ico', format='ICO', sizes=[(16,16),(24,24),(32,32),(48,48),(64,64),(128,128),(256,256)])
