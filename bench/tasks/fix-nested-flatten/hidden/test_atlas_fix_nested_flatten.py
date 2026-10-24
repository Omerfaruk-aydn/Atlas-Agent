import unittest
from core import *

class AtlasFixNestedFlatten(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(flatten([1,[2,(3,)],"ab",{"x":1}]),[1,2,3,"ab",{"x":1}])
        self.assertEqual(flatten([[],()]),[])
