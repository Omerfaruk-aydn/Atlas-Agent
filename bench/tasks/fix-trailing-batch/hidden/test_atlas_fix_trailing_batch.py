import unittest
from core import *

class AtlasFixTrailingBatch(unittest.TestCase):
    def test_contract(self):
        self.assertEqual(batches([1,2,3,4,5],2),[[1,2],[3,4],[5]])
        self.assertEqual(batches([],2),[])
        with self.assertRaises(ValueError): batches([1],0)
