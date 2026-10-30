import unittest
from core import *

class AtlasFixNewlineNormalization(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(normalize(" a\r\nb\rc\n")," a\nb\nc\n")
        import config
        self.assertEqual(config.MAX_RECORDS,64)
