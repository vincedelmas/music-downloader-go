#!/usr/bin/env python3
"""Build a self-contained desktop distribution on the target OS. Requires Go and Python 3."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parent.parent
REPO = 'vincedelmas/music-downloader-go'
FFMPEG_TAG = 'b6.1.1'
TARGETS = {
 ('linux','amd64'): ('yt-dlp_linux','linux-x64','x86_64-unknown-linux-gnu'),
 ('windows','amd64'): ('yt-dlp.exe','win32-x64','x86_64-pc-windows-msvc'),
 ('darwin','amd64'): ('yt-dlp_macos','darwin-x64','x86_64-apple-darwin'),
 ('darwin','arm64'): ('yt-dlp_macos','darwin-arm64','aarch64-apple-darwin'),
}

def request(url):
    headers = {'User-Agent':'music-downloader-build', 'Accept':'application/vnd.github+json'}
    token = os.environ.get('GH_TOKEN')
    if token and url.startswith('https://api.github.com/'):
        headers['Authorization'] = 'Bearer ' + token
    return urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=180)

def release(repo, tag=None):
    endpoint = 'tags/' + tag if tag else 'latest'
    with request('https://api.github.com/repos/'+repo+'/releases/'+endpoint) as r:
        return json.load(r)

def download(url, dest, digest=None):
    print('Downloading', dest.name, flush=True)
    pending = dest.with_name(dest.name+'.pending')
    try:
        with request(url) as r, pending.open('wb') as f:
            shutil.copyfileobj(r, f)
        if digest and digest.startswith('sha256:'):
            with pending.open('rb') as f:
                actual=hashlib.file_digest(f, 'sha256').hexdigest()
            if actual != digest[7:]:
                raise RuntimeError('Checksum mismatch: '+dest.name)
        pending.replace(dest)
    finally:
        pending.unlink(missing_ok=True)

def fetch_asset(rel, name, dest):
    asset = next((a for a in rel['assets'] if a['name']==name), None)
    if not asset:
        raise RuntimeError('Missing release asset: '+name)
    download(asset['browser_download_url'], dest, asset.get('digest'))

def run(*args, **kwargs):
    print('+', ' '.join(map(str,args)), flush=True)
    subprocess.run(list(map(str,args)), cwd=ROOT, check=True, **kwargs)

def bundle_tools(dest, goos, arch):
    dest.mkdir(parents=True, exist_ok=True)
    yt, ffplatform, triple = TARGETS[goos,arch]
    suffix = '.exe' if goos=='windows' else ''
    versions = {}
    for repo, tag, files in [
      ('yt-dlp/yt-dlp',None,{yt:'yt-dlp'+suffix}),
      ('eugeneware/ffmpeg-static',FFMPEG_TAG,{'ffmpeg-'+ffplatform:'ffmpeg'+suffix,'ffprobe-'+ffplatform:'ffprobe'+suffix,ffplatform+'.LICENSE':'FFmpeg-LICENSE.txt',ffplatform+'.README':'FFmpeg-SOURCE.txt'}),
      ('denoland/deno',None,{'deno-'+triple+'.zip':'deno.zip'}),
    ]:
        rel = release(repo,tag)
        versions[repo] = rel['tag_name']
        for source,target in files.items():
            fetch_asset(rel,source,dest/target)
    with zipfile.ZipFile(dest/'deno.zip') as z:
        with z.open('deno'+suffix) as src, (dest/('deno'+suffix)).open('wb') as dst:
            shutil.copyfileobj(src,dst)
    (dest/'deno.zip').unlink()
    for name in ['yt-dlp','ffmpeg','ffprobe','deno']:
        executable = dest/(name+suffix)
        executable.chmod(0o755)
        run(executable, '-version' if name in ('ffmpeg','ffprobe') else '--version', stdout=subprocess.DEVNULL)
    (dest/'versions.json').write_text(json.dumps(versions,indent=2)+'\n')

def notices(dest):
    dest.mkdir(parents=True,exist_ok=True)
    if (ROOT/'build'/'go-licenses').is_dir():
        shutil.copytree(ROOT/'build'/'go-licenses',dest/'go')
    shutil.copy2(ROOT/'THIRD_PARTY_NOTICES.txt',dest/'THIRD_PARTY_NOTICES.txt')
    for repo,ref,name in [('yt-dlp/yt-dlp','master','LICENSE'),('denoland/deno','main','LICENSE.md'),('eugeneware/ffmpeg-static','master','LICENSE')]:
        download('https://raw.githubusercontent.com/'+repo+'/'+ref+'/'+name,dest/(repo.split('/')[0]+'-LICENSE.txt'))

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--version',default='dev')
    p.add_argument('--tools-only',action='store_true',help='Populate ./tools for go run/build development')
    args=p.parse_args()
    if not re.fullmatch(r'(?:v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?|dev)',args.version):
        p.error('Version must be dev or a semantic version')
    goos={'Linux':'linux','Windows':'windows','Darwin':'darwin'}[platform.system()]
    arch={'x86_64':'amd64','AMD64':'amd64','arm64':'arm64','aarch64':'arm64'}[platform.machine()]
    if (goos,arch) not in TARGETS:
        p.error('Unsupported build target: '+goos+'/'+arch)
    if args.tools_only:
        bundle_tools(ROOT/'tools',goos,arch)
        return
    stage=ROOT/'build'/'package'
    if stage.exists(): shutil.rmtree(stage)
    stage.mkdir(parents=True)
    out=ROOT/'dist';out.mkdir(exist_ok=True)
    base='music-downloader-'+args.version+'-'+goos+'-'+arch
    exe=stage/('music-downloader.exe' if goos=='windows' else 'music-downloader')
    flags='-s -w -X main.version='+args.version
    if goos=='windows': flags+=' -H=windowsgui'
    resource=ROOT/'icon_windows_amd64.syso'
    if goos=='windows':
        rc=ROOT/'build'/'icon.rc'; rc.write_text('1 ICON "'+(ROOT/'assets'/'icon.ico').as_posix()+'"\n')
        run('windres','-i',rc,'-o',resource,'-O','coff')
    try:
        run('go','build','-trimpath','-ldflags',flags,'-o',exe,'.')
    finally:
        if goos=='windows': resource.unlink(missing_ok=True)
    bundle_tools(stage/'tools',goos,arch)
    notices(stage/'licenses')
    shutil.copy2(ROOT/'assets'/'icon.png',stage/'icon.png')
    shutil.copy2(ROOT/'README.md',stage/'README.md')
    if goos=='linux':
        shutil.copy2(ROOT/'scripts'/'install.sh',stage/'install.sh')
        (stage/'install.sh').chmod(0o755)
        with tarfile.open(out/(base+'.tar.gz'),'w:gz') as tar: tar.add(stage,arcname='music-downloader')
    elif goos=='windows':
        with zipfile.ZipFile(out/(base+'.zip'),'w',zipfile.ZIP_DEFLATED) as z:
            for f in stage.rglob('*'):
                if f.is_file(): z.write(f,Path('music-downloader')/f.relative_to(stage))
        compiler=shutil.which('iscc') or r'C:\Program Files (x86)\Inno Setup 6\ISCC.exe'
        run(compiler,'/DAppVersion='+args.version.lstrip('v'),'/DArtifactName='+base+'-setup',ROOT/'scripts'/'windows.iss')
    else:
        app=ROOT/'build'/'Music Downloader.app'
        if app.exists(): shutil.rmtree(app)
        contents=app/'Contents';macos=contents/'MacOS';resources=contents/'Resources'
        macos.mkdir(parents=True);resources.mkdir()
        shutil.move(exe,macos/'music-downloader')
        for name in ['tools','licenses']: shutil.move(stage/name,resources/name)
        # iconutil requires a full iconset, generated from the application PNG.
        icons=ROOT/'build'/'AppIcon.iconset';icons.mkdir(exist_ok=True)
        for size in [16,32,128,256,512]:
            for scale in [1,2]:
                name=f'icon_{size}x{size}'+('@2x' if scale==2 else '')+'.png'
                run('sips','-z',size*scale,size*scale,ROOT/'assets'/'icon.png','--out',icons/name,stdout=subprocess.DEVNULL)
        run('iconutil','-c','icns',icons,'-o',resources/'AppIcon.icns')
        with (contents/'Info.plist').open('wb') as f:
            plistlib.dump({'CFBundleExecutable':'music-downloader','CFBundleIdentifier':'io.github.vincedelmas.music-downloader','CFBundleName':'Music Downloader','CFBundleDisplayName':'Music Downloader','CFBundlePackageType':'APPL','CFBundleIconFile':'AppIcon','CFBundleShortVersionString':args.version.lstrip('v') if args.version!='dev' else '0.0.0','CFBundleVersion':args.version.lstrip('v') if args.version!='dev' else '0.0.0','NSHighResolutionCapable':True,'LSMinimumSystemVersion':'12.0'},f)
        # Ad-hoc signatures make local Apple Silicon code executable; no Developer ID/notarization.
        for tool in ['yt-dlp','ffmpeg','ffprobe','deno']: run('codesign','--force','--sign','-',resources/'tools'/tool)
        run('codesign','--force','--deep','--sign','-',app)
        dmgroot=ROOT/'build'/'dmg';shutil.rmtree(dmgroot,ignore_errors=True);dmgroot.mkdir()
        shutil.copytree(app,dmgroot/app.name);(dmgroot/'Applications').symlink_to('/Applications')
        target=out/(base+'.dmg');target.unlink(missing_ok=True)
        run('hdiutil','create','-volname','Music Downloader','-srcfolder',dmgroot,'-ov','-format','UDZO',target)
    print('Packages ready in',out)

if __name__=='__main__': main()
