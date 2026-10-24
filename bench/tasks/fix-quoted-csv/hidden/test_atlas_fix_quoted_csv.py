import unittest
from core import *

class AtlasFixQuotedCsv(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(row('a,"b,c","d""e",'),["a","b,c",'d"e',""])
        self.assertEqual(row("a,,b"),["a","","b"])
