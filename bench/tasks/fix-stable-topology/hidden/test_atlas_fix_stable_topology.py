import unittest
from core import *

class AtlasFixStableTopology(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(order({"a":["b"],"b":[],"c":[]}),["b","a","c"])
        with self.assertRaises(ValueError): order({"a":["a"]})
        with self.assertRaises(ValueError): order({"a":["missing"]})
