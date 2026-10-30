import csv
def row(s):
    return next(csv.reader([s]))
