import unittest
from core import *

class AtlasFixDecimalTotal(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(total(["1.005"]),"1.01")
        self.assertEqual(total(["0.10","0.20"]),"0.30")
        self.assertEqual(total([]),"0.00")
