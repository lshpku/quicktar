import os
import re
import json
import time
import base64
import hashlib
import argparse
import threading
from html import unescape
from urllib.parse import urljoin, urlparse
from subprocess import Popen, DEVNULL, PIPE
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from http.client import HTTPConnection

GOLDEN_RATIO = (3 - 5**.5) / 2
MAX_PIXELS = 10 ** 5
RE_HREF = re.compile(r'<D:href>(.*?)</D:href>')
RE_CLEN = re.compile(r'<D:getcontentlength>(\d+)')
IMAGE_CODEC_NAMES = set('mjpeg png bmp tiff'.split())
ALPHA_FILTER = ('format=rgba,geq='
                r'r=alpha(X\,Y)/255*r(X\,Y)+255-alpha(X\,Y):'
                r'g=alpha(X\,Y)/255*g(X\,Y)+255-alpha(X\,Y):'
                r'b=alpha(X\,Y)/255*b(X\,Y)+255-alpha(X\,Y)')

parser = argparse.ArgumentParser()
parser.add_argument('--host', default='127.0.0.1')
parser.add_argument('port', nargs='?', type=int, default=8000)
parser.add_argument('--to', default='http://127.0.0.1:8080')
parser.add_argument('--cache', default='webdav_proxy.cache')
parser.add_argument('-j', '--workers', type=int, default=4)
parser.add_argument('-p', '--password')


class TaskQueue:
    '''
    TaskQueue is a thread-safe and unique queue that organizes tasks in the
    most-recently-submitted order.
    '''
    INPROC = 1

    def __init__(self):
        self.cond = threading.Condition()
        self.closed = False

        # Link list nodes (elem, prev, succ)
        self.head = [None, None, None]
        self.tail = [None, self.head, None]
        self.head[2] = self.tail

        # Task to link list node, or None if the task is in processing
        self.map = {}

    def putmany(self, tasks):
        '''Summits tasks to the queue.'''
        with self.cond:
            assert not self.closed
            for task in tasks:
                d = [task, None, None]
                # Skip tasks that are in processing.
                if (c := self.map.setdefault(task, d)) == self.INPROC:
                    continue
                # Remove task from list if it exists.
                elif c is not d:
                    _, prev, succ = c
                    prev[2], succ[1] = succ, prev
                # Insert task right after head.
                c[1], c[2] = self.head, self.head[2]
                c[1][2] = c[2][1] = c
                self.cond.notify()

    def get(self):
        '''Gets one task to process.
        The returned task is marked busy but not removed from the queue.
        You should call done(task) when the task finishes.
        Returns None if the queue is closed.'''
        with self.cond:
            # Wait until the set is not empty or is closed.
            if self.closed:
                return None
            while self.head[2] is self.tail:
                self.cond.wait()
                if self.closed:
                    return None
            # Pop the MRU item.
            task, prev, succ = self.head[2]
            prev[2], succ[1] = succ, prev
            self.map[task] = self.INPROC
            return task

    def done(self, task):
        '''Remove task from the queue.'''
        with self.cond:
            if self.closed:
                return
            if self.map[task] != self.INPROC:
                raise Exception('task is not under processing')
            del self.map[task]

    def close(self):
        with self.cond:
            self.head = self.tail = self.map = None
            self.closed = True
            self.cond.notify_all()


def take_snapshot(url, tmpfile, ss=None, vf=None):
    cmd = 'ffmpeg -hide_banner -threads 1'.split()
    if ss is not None:
        cmd += ['-ss', str(ss)]
    cmd += ['-i', url]
    cmd += '-map v:0 -map_metadata -1 -frames:v 1 -update 1 -y'.split()
    if vf:
        cmd += ['-vf', vf]
    cmd += '-pix_fmt bgr24 -y'.split()
    cmd += [tmpfile]

    p = Popen(cmd, stdin=DEVNULL, stdout=DEVNULL, stderr=PIPE)
    data = bytearray()
    while b := p.stderr.read(65536):
        data += b
    p.wait()  # check output file instead of return value

    has_output = False
    for line in data.decode().splitlines():
        if has_output:
            if line.startswith('  Stream #0:0: Video: bmp'):
                if m := re.search(r'(\d+)x(\d+)', line):
                    return int(m.group(1)), int(m.group(2))
        elif line.startswith('Output #0, image2, to'):
            has_output = True
    return None


def generate_thumbnail(url, tmpfile):
    # Get codec and duration of the first video stream
    t0 = time.time()
    cmd = 'ffprobe -v quiet -threads 1'.split()
    cmd += '-print_format json -show_streams -show_format'.split()
    cmd.append(url)
    p = Popen(cmd, stdin=DEVNULL, stdout=PIPE, stderr=DEVNULL)
    try:
        info = json.load(p.stdout)
    except ValueError:
        p.wait()
        raise
    if p.wait():
        raise RuntimeError('ffprobe failed')

    video = None
    for stream in info['streams']:
        if stream['codec_type'] == 'video':
            video = stream
            break
    if video is None:
        raise Exception("no video stream")

    codec_name = video['codec_name']
    if codec_name in IMAGE_CODEC_NAMES:
        # don't set time for image
        duration = snapshot_time = None
    else:
        # prefer stream specs
        if (duration := video.get('duration')) is None:
            duration = info['format']['duration']
        duration = float(duration)
        snapshot_time = duration * GOLDEN_RATIO

    pix_fmt = video['pix_fmt']
    video_filter = None
    if codec_name == 'png' and pix_fmt != 'rgb24':
        video_filter = ALPHA_FILTER

    # Take snapshot
    # The stream may contain no frame at snapshot_time. If so, reduce
    # snapshot_time by GOLDEN_RATIO.
    t1 = time.time()
    while True:
        size = take_snapshot(url, tmpfile, snapshot_time, video_filter)
        if os.path.exists(tmpfile):
            break
        snapshot_time *= GOLDEN_RATIO
        if snapshot_time < 1:
            snapshot_time = None

    # Compute output size
    w, h = size
    if w * h > MAX_PIXELS:
        # Make the length of the short side as a multiple of 16
        r = (MAX_PIXELS / (w * h)) ** .5
        ss = min(w, h)
        r = (ss * r // 16 * 16) / ss
        w, h = round(w * r), round(h * r)

    # Resize snapshot
    t2 = time.time()
    cmd = 'ffmpeg -hide_banner -loglevel quiet -threads 1'.split()
    cmd += ['-i', tmpfile]
    cmd += ['-vf', f'scale={w}x{h}']
    cmd += '-frames:v 1 -update 1 -f mjpeg pipe:1'.split()
    p = Popen(cmd, stdin=DEVNULL, stdout=PIPE, stderr=DEVNULL)
    data = bytearray()
    while b := p.stdout.read(65536):
        data += b
    p.wait()
    os.remove(tmpfile)
    if p.returncode:
        raise RuntimeError('ffmpeg failed to resize snapshot')

    t3 = time.time()
    print('[genthumb] probe: %.3fms snapshoot: %.3fms resize: %.3fms' % (
        (t1 - t0) * 1000, (t2 - t1) * 1000, (t3 - t2) * 1000), url, bool(video_filter))
    return {
        'codec_name': codec_name,
        'duration': duration,
        'thumbnail': bytes(data),
        'pix_fmt': pix_fmt
    }


def generate_thumbnail_thread():
    import traceback
    uid = base64.urlsafe_b64encode(os.urandom(15))
    tmpfile = '/tmp/webdav_proxy-' + uid.decode() + '.bmp'
    IGNORE_EXT = set('''
        bc%21 txt torrent srt downloading nfo zip rar ini
    '''.split())

    while path := task_queue.get():
        url = urljoin(args.to, path)
        try:
            data = generate_thumbnail(url, tmpfile)
        except Exception:
            ext = url.rfind('.') + 1
            if ext and url[ext:].lower() not in IGNORE_EXT:
                exc = traceback.format_exc()
                with open('error.log', 'a') as f:
                    f.write(url + '\n' + exc + '\n')
            data = -1
        uid = hashlib.sha256(password + path.encode()).digest()
        db[uid] = data
        task_queue.done(path)  # must call done after updating db


def convert_picture(url, png=False, alpha=False):
    cmd = 'ffmpeg -hide_banner -loglevel quiet -threads 1'.split()
    cmd += ['-i', url]
    cmd += '-frames:v 1 -update 1 -c:v png -pix_fmt'.split()
    if png:
        cmd += ['rgba']
    else:
        cmd += ['rgb24']
    cmd += '-compression_level 0 -f image2pipe -'.split()
    t0 = time.time()
    p = Popen(cmd, stdin=DEVNULL, stdout=PIPE, stderr=DEVNULL)
    data = bytearray()
    while b := p.stdout.read(65536):
        data += b
    if p.wait():
        raise RuntimeError('ffmpeg failed to convert picture')
    t1 = time.time()
    print('[convjpeg] convert: %.3fms' % ((t1 - t0) * 1000))
    return bytes(data)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        # Serve index
        if self.path == '/':
            with open('webdav_proxy_index.html', 'rb') as f:
                data = f.read()
            self.send_response(200)
            self.send_header('Content-Length', str(len(data)))
            self.send_header('Content-Type', 'text/html')
            self.end_headers()
            self.wfile.write(data)
            return

        # Serve dir
        if self.path.startswith('/dir/'):
            self.serve_dir(self.path[4:])
            return

        # Serve thumbnails
        if self.path.startswith('/img/'):
            self.serve_img(self.path[5:] + '=')
            return

        # Open videos in local player
        if self.path.startswith('/open/'):
            url = urljoin(args.to, self.path[5:])
            p = Popen(['open', '-a', '/Applications/IINA.app', url])
            if p.wait():
                self.send_error(500)
                return
            self.send_response(200)
            self.send_header('Content-Length', '0')
            self.end_headers()
            return

        # Open pictures in browser
        if self.path.startswith('/openpic/'):
            self.serve_pic(self.path[8:])

        # Serve static files
        if self.path.startswith('/static/'):
            path = self.path[8:]
            with open(path, 'rb') as f:
                data = f.read()
            self.send_response(200)
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return

        self.send_error(404)

    def serve_dir(self, path):
        # Search directory tree
        t0 = time.time()
        folders = [fn for fn in path.split('/') if fn]
        dirname = ('/' + '/'.join(folders)) if folders else ''
        cur = root
        for fn in folders:
            if (cur := cur.get(fn)) is None:
                self.send_error(404)
                return
        if isinstance(cur, int):  # reject to serve file
            self.send_error(403)
            return

        # Sort directories and files seperately
        dirs, files = [], []
        for fn, fd in cur.items():
            p = (fn.lower(), fn)
            if isinstance(fd, int):
                files.append({'name': fn, 'p': p, 'size': fd})
            else:
                dirs.append({'name': fn + '/', 'p': p})

        dirs.sort(key=lambda p: p['p'])
        files.sort(key=lambda p: p['p'])

        # Search database
        t1 = time.time()
        unprocessed_files = []
        for fn in files:
            del fn['p']
            fp = dirname + '/' + fn['name']
            uid = hashlib.sha256(password + fp.encode()).digest()
            if (meta := db.get(uid)) is None:
                unprocessed_files.append(fp)
            elif meta == -1:
                fn['img'] = ''
            else:
                fn['img'] = base64.urlsafe_b64encode(uid)[:-1].decode()
                if meta['duration'] is not None:
                    fn['duration'] = meta['duration']
                if meta['codec_name'] in IMAGE_CODEC_NAMES:
                    fn['ispic'] = True
        task_queue.putmany(reversed(unprocessed_files))
        t2 = time.time()

        # Send response
        data = []
        for fn in dirs:
            del fn['p']
            data.append(fn)
        data += files
        data = json.dumps(data, separators=(',', ':')).encode()
        self.send_response(200)
        self.send_header('Content-Length', str(len(data)))
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(data)
        t3 = time.time()
        print('[servedir] filetree: %.3fms database: %.3fms sendresp: %.3fms' %
              ((t1 - t0) * 1000, (t2 - t1) * 1000, (t3 - t2) * 1000))

    def serve_img(self, uid):
        try:
            uid = base64.urlsafe_b64decode(uid)
        except ValueError:
            self.send_error(400)
            return

        if (meta := db.get(uid)) is None:
            self.send_error(404)
            return
        data = meta['thumbnail']

        self.send_response(200)
        self.send_header('Content-Length', str(len(data)))
        self.send_header('Content-Type', 'image/jpeg')
        self.send_header('Cache-Control', 'max-age=3600')
        self.end_headers()
        self.wfile.write(data)

    def serve_pic(self, path):
        uid = hashlib.sha256(password + path.encode()).digest()
        if (meta := db.get(uid)) is None:
            self.send_error(404)
            return

        png = False
        if meta['codec_name'] == 'png' and meta['pix_fmt'] != 'rgb24':
            png = True

        if (data := converted_pics.get(uid)) is None:
            try:
                data = convert_picture(urljoin(args.to, path), png)
            except RuntimeError:
                data = ''
            converted_pics[uid] = data

        if not data:
            self.send_error(500)
            return

        self.send_response(200)
        self.send_header('Content-Length', str(len(data)))
        if png:
            self.send_header('Content-Type', 'image/png')
        else:
            self.send_header('Content-Type', 'image/png')
        self.end_headers()
        self.wfile.write(data)


def expand_small_dirs(root):
    child_dirs = []
    for fn, fd in root.items():
        if isinstance(fd, dict):
            child_dirs.append(fn)
            expand_small_dirs(fd)
    for fn in child_dirs:
        if len(root[fn]) == 0:
            del root[fn]
        elif len(root[fn]) < 10:
            fd = root.pop(fn)
            for child_fn, child_fd in fd.items():
                root[fn + '%2F' + child_fn] = child_fd


if __name__ == "__main__":
    args = parser.parse_args()
    password = os.urandom(32)
    db = {}  # uid: data
    converted_pics = {}  # uid: data

    # Request WebDAV server
    print('requesting WebDAV server')
    conn = HTTPConnection(urlparse(args.to).netloc)
    conn.request("PROPFIND", '/')
    resp = conn.getresponse()
    assert resp.status == 207
    resp = resp.read().decode()

    # Build file tree
    print('building file tree')
    root = {}  # dir -> {}, file -> size
    file_urls = []
    hrefs = RE_HREF.findall(resp)
    clens = iter(RE_CLEN.findall(resp))
    for p in hrefs:
        if p == '/':
            continue
        p = unescape(p)
        cur = root
        folders = p.split('/')
        for fn in folders[1:-1]:
            cur = cur.setdefault(fn, {})
        if fn := folders[-1]:  # file
            file_urls.append(urljoin(args.to, p))
            cur[fn] = int(next(clens))
    assert next(clens, None) is None

    # Expand small directories
    expand_small_dirs(root)

    # Start ffmpeg worker threads
    task_queue = TaskQueue()
    threads = []
    for i in range(args.workers):
        t = threading.Thread(target=generate_thumbnail_thread)
        t.start()
        threads.append(t)
    print('running', args.workers, 'workers')

    # Start http server
    addr = (args.host, args.port)
    server = ThreadingHTTPServer(addr, Handler)
    print('listening on', addr)

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print('quiting')

    # Stop workers & close database
    task_queue.close()
    for t in threads:
        t.join()
