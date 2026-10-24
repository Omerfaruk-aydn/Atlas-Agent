import unittest
from core import *

class AtlasFixPlaceholderExpansion(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(expand("${A}/${MISSING}/A/$",{"A":"yes"}),"yes/${MISSING}/A/$")
        self.assertEqual(expand("${EMPTY}",{"EMPTY":""}),"")
