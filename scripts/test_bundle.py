import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('bundle',Path(__file__).with_name('bundle.py'))
bundle=importlib.util.module_from_spec(spec)
spec.loader.exec_module(bundle)

class BundleTests(unittest.TestCase):
    def test_missing_upstream_asset_fails(self):
        with self.assertRaisesRegex(RuntimeError,'Missing release asset'):
            bundle.fetch_asset({'assets':[]},'ffmpeg',Path('unused'))

    def test_checksum_failure_keeps_existing_file(self):
        class Response:
            def __enter__(self): return self
            def __exit__(self,*args): pass
            def read(self,n):
                if hasattr(self,'done'): return b''
                self.done=True
                return b'corrupt executable'
        with tempfile.TemporaryDirectory() as root:
            dest=Path(root)/'tool';dest.write_bytes(b'working executable')
            with patch.object(bundle,'request',return_value=Response()):
                with self.assertRaisesRegex(RuntimeError,'Checksum mismatch'):
                    bundle.download('https://example.org/tool',dest,'sha256:'+'0'*64)
            self.assertEqual(dest.read_bytes(),b'working executable')
            self.assertEqual(list(Path(root).iterdir()),[dest])

    def test_each_target_uses_standalone_downloads(self):
        self.assertEqual(len(bundle.TARGETS),4)
        for (goos,arch),(yt,ff,triple) in bundle.TARGETS.items():
            self.assertNotEqual(yt,'yt-dlp') # Generic Unix zipimport needs a system Python.
            self.assertTrue(ff)
            self.assertTrue(triple)

if __name__=='__main__': unittest.main()
