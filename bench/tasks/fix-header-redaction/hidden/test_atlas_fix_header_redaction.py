import unittest
from core import *

class AtlasFixHeaderRedaction(unittest.TestCase):
    def test_contract(self):
        h={"authorization":"dummy","COOKIE":"fixture","Accept":"text/plain"}
        self.assertEqual(mask(h),{"authorization":"[REDACTED]","COOKIE":"[REDACTED]","Accept":"text/plain"})
        self.assertEqual(h["COOKIE"],"fixture")
