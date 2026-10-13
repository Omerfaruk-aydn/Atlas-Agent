		case err != nil:
			l.err = err
		case !gone:
			l.err = fmt.Errorf("%w (%s)", ErrResidual, l.plan.URL())
		default:
			l.err = l.m.removeRecord()
		}
		l.progress.Set(PhaseStopped)
	})
	return l.err
}
