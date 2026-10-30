import unittest
from core import *

class AtlasFixDurationDecimal(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(milliseconds("1.25s"),1250)
        self.assertEqual(milliseconds(" 2ms "),2)
        self.assertEqual(milliseconds("0.5m"),30000)
        with self.assertRaises(ValueError): milliseconds("0.1ms")
        with self.assertRaises(ValueError): milliseconds("-1s")
