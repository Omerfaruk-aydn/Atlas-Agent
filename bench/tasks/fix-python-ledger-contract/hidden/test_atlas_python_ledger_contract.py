import unittest
import config
from decimal import Decimal
from parser import parse
from ledger import total

class AtlasPythonLedgerContract(unittest.TestCase):
    def test_parser_and_total_agree(self):
        self.assertEqual(parse(" a ,0.005\nb,0.005\nc,-0.005\n"), [("a", Decimal("0.005")), ("b", Decimal("0.005")), ("c", Decimal("-0.005"))])
        self.assertEqual(total("a,0.005\nb,0.005"), "0.01")
        self.assertEqual(total("a,-0.005"), "-0.01")
        self.assertEqual(total(""), "0.00")
    def test_contract_rejections_and_scope(self):
        for text in ["a,1\n a ,2", "a,1,2", ",3", "a,NaN", "a,Infinity", "a,garbage", "\n".join(f"{i},1" for i in range(65))]:
            with self.subTest(text=text), self.assertRaises(ValueError):
                parse(text)
        self.assertEqual(len(parse("\n".join(f"{i},1" for i in range(64)))), 64)
        self.assertEqual(config.MAX_RECORDS, 64)
