import unittest
from core import *

class AtlasFixRecursiveMerge(unittest.TestCase):
    def test_contract(self):
        a={"x":{"a":1}}; b={"x":{"b":2},"list":[1]}
        r=merge(a,b)
        self.assertEqual(r,{"x":{"a":1,"b":2},"list":[1]})
        r["x"]["a"]=9; r["list"].append(2)
        self.assertEqual(a,{"x":{"a":1}}); self.assertEqual(b["list"],[1])
