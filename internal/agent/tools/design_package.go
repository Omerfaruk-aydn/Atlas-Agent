			}
		}
		prototype = prototype || a.Passed && a.PrototypeSteps > 0 && a.AssertionCount > 0
	}
	if !prototype {
		return "partial"
	}
	return "complete"
}
