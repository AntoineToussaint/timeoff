import { useState, useMemo } from 'react';
import {
  format,
  startOfMonth,
  endOfMonth,
  startOfWeek,
  endOfWeek,
  addDays,
  addMonths,
  subMonths,
  isSameMonth,
  isSameDay,
  isWithinInterval,
  isWeekend,
  isBefore,
} from 'date-fns';
import { ChevronLeft, ChevronRight, X } from 'lucide-react';

const styles = {
  overlay: {
    position: 'fixed' as const,
    inset: 0,
    background: 'rgba(0, 0, 0, 0.75)',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    zIndex: 100,
  },
  picker: {
    background: 'var(--bg-card)',
    border: '1px solid var(--border)',
    borderRadius: '16px',
    padding: '1.5rem',
    width: '380px',
    boxShadow: '0 25px 50px -12px rgba(0, 0, 0, 0.5)',
  },
  header: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: '1.5rem',
  },
  title: {
    fontSize: '1.125rem',
    fontWeight: 600,
  },
  closeBtn: {
    background: 'transparent',
    border: 'none',
    color: 'var(--text-muted)',
    cursor: 'pointer',
    padding: '0.25rem',
    borderRadius: '6px',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
  },
  monthNav: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: '1rem',
  },
  monthLabel: {
    fontSize: '1rem',
    fontWeight: 600,
  },
  navBtn: {
    background: 'var(--bg-hover)',
    border: '1px solid var(--border)',
    borderRadius: '8px',
    color: 'var(--text)',
    cursor: 'pointer',
    padding: '0.5rem',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    transition: 'all 0.2s',
  },
  weekdays: {
    display: 'grid',
    gridTemplateColumns: 'repeat(7, 1fr)',
    gap: '2px',
    marginBottom: '0.5rem',
  },
  weekday: {
    textAlign: 'center' as const,
    fontSize: '0.75rem',
    fontWeight: 600,
    color: 'var(--text-muted)',
    padding: '0.5rem 0',
  },
  days: {
    display: 'grid',
    gridTemplateColumns: 'repeat(7, 1fr)',
    gap: '2px',
  },
  day: {
    aspectRatio: '1',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    fontSize: '0.875rem',
    borderRadius: '8px',
    cursor: 'pointer',
    transition: 'all 0.15s',
    border: 'none',
    background: 'transparent',
    color: 'var(--text)',
  },
  summary: {
    marginTop: '1.5rem',
    padding: '1rem',
    background: 'var(--bg-dark)',
    borderRadius: '10px',
  },
  summaryRow: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: '0.5rem',
  },
  summaryLabel: {
    fontSize: '0.875rem',
    color: 'var(--text-muted)',
  },
  summaryValue: {
    fontSize: '0.875rem',
    fontWeight: 600,
  },
  actions: {
    display: 'flex',
    gap: '0.75rem',
    marginTop: '1.5rem',
  },
  btn: {
    flex: 1,
    padding: '0.75rem 1rem',
    borderRadius: '8px',
    fontSize: '0.875rem',
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'all 0.2s',
    border: 'none',
  },
  btnPrimary: {
    background: 'var(--accent)',
    color: 'white',
  },
  btnSecondary: {
    background: 'var(--bg-hover)',
    color: 'var(--text)',
    border: '1px solid var(--border)',
  },
  quickSelect: {
    display: 'flex',
    gap: '0.5rem',
    marginBottom: '1rem',
    flexWrap: 'wrap' as const,
  },
  quickBtn: {
    padding: '0.375rem 0.75rem',
    borderRadius: '6px',
    fontSize: '0.75rem',
    fontWeight: 500,
    cursor: 'pointer',
    transition: 'all 0.2s',
    border: '1px solid var(--border)',
    background: 'var(--bg-dark)',
    color: 'var(--text-muted)',
  },
};

interface DateRangePickerProps {
  onSelect: (dates: Date[]) => void;
  onClose: () => void;
  minDate?: Date;
  availableDays: number;
}

export function DateRangePicker({ onSelect, onClose, minDate, availableDays }: DateRangePickerProps) {
  const [currentMonth, setCurrentMonth] = useState(new Date());
  const [startDate, setStartDate] = useState<Date | null>(null);
  const [endDate, setEndDate] = useState<Date | null>(null);
  const [hoverDate, setHoverDate] = useState<Date | null>(null);

  const today = new Date();
  const effectiveMinDate = minDate || today;

  // Generate calendar days
  const calendarDays = useMemo(() => {
    const monthStart = startOfMonth(currentMonth);
    const monthEnd = endOfMonth(currentMonth);
    const calStart = startOfWeek(monthStart);
    const calEnd = endOfWeek(monthEnd);

    const days: Date[] = [];
    let day = calStart;
    while (day <= calEnd) {
      days.push(day);
      day = addDays(day, 1);
    }
    return days;
  }, [currentMonth]);

  // Calculate workdays in selection
  const selectedWorkdays = useMemo(() => {
    if (!startDate) return [];
    const end = endDate || hoverDate || startDate;
    const actualStart = isBefore(startDate, end) ? startDate : end;
    const actualEnd = isBefore(startDate, end) ? end : startDate;

    const days: Date[] = [];
    let current = new Date(actualStart);
    const endTime = actualEnd.getTime();
    
    while (current.getTime() <= endTime) {
      if (!isWeekend(current) && !isBefore(current, effectiveMinDate)) {
        days.push(new Date(current));
      }
      current = addDays(current, 1);
    }
    return days;
  }, [startDate, endDate, hoverDate, effectiveMinDate]);

  const handleDayClick = (day: Date) => {
    if (isBefore(day, effectiveMinDate)) return;

    if (!startDate || (startDate && endDate)) {
      // Start new selection
      setStartDate(day);
      setEndDate(null);
    } else {
      // Complete selection
      if (isBefore(day, startDate)) {
        setEndDate(startDate);
        setStartDate(day);
      } else {
        setEndDate(day);
      }
    }
  };

  const handleConfirm = () => {
    if (selectedWorkdays.length > 0) {
      onSelect(selectedWorkdays);
    }
  };

  const handleQuickSelect = (days: number) => {
    let start = new Date(effectiveMinDate);
    // Find next workday
    while (isWeekend(start)) {
      start = addDays(start, 1);
    }
    
    // Find end date to get desired workdays
    let count = 1; // start counts as first day
    let end = new Date(start);
    while (count < days) {
      end = addDays(end, 1);
      if (!isWeekend(end)) {
        count++;
      }
    }
    
    setStartDate(start);
    setEndDate(end);
    setHoverDate(null);
  };

  const getDayStyle = (day: Date) => {
    const base = { ...styles.day };
    const isDisabled = isBefore(day, effectiveMinDate);
    const isCurrentMonth = isSameMonth(day, currentMonth);
    const isStart = startDate && isSameDay(day, startDate);
    const isEnd = endDate && isSameDay(day, endDate);
    const rangeEnd = endDate || hoverDate;
    let isInRange = false;
    if (startDate && rangeEnd) {
      const rangeStart = isBefore(startDate, rangeEnd) ? startDate : rangeEnd;
      const rangeFinish = isBefore(startDate, rangeEnd) ? rangeEnd : startDate;
      isInRange = isWithinInterval(day, { start: rangeStart, end: rangeFinish });
    }
    const isWeekendDay = isWeekend(day);
    const isToday = isSameDay(day, today);

    return {
      ...base,
      opacity: !isCurrentMonth ? 0.3 : isDisabled ? 0.4 : 1,
      cursor: isDisabled ? 'not-allowed' : 'pointer',
      color: isDisabled 
        ? 'var(--text-muted)' 
        : isStart || isEnd 
          ? 'white' 
          : isInRange
            ? 'white'
            : isWeekendDay 
              ? 'var(--text-muted)' 
              : 'var(--text)',
      background: isStart || isEnd 
        ? 'var(--accent)' 
        : isInRange && !isWeekendDay
          ? '#6366f1'
          : isInRange && isWeekendDay
            ? '#818cf8'
            : 'transparent',
      fontWeight: isToday ? 700 : isStart || isEnd ? 600 : 400,
      borderRadius: isStart 
        ? '8px 0 0 8px' 
        : isEnd 
          ? '0 8px 8px 0' 
          : isInRange 
            ? '0' 
            : '8px',
      textDecoration: isWeekendDay ? 'line-through' : 'none',
    };
  };

  return (
    <div style={styles.overlay} onClick={onClose}>
      <div style={styles.picker} onClick={(e) => e.stopPropagation()}>
        <div style={styles.header}>
          <span style={styles.title}>Select Dates</span>
          <button style={styles.closeBtn} onClick={onClose}>
            <X size={20} />
          </button>
        </div>

        {/* Quick Select */}
        <div style={styles.quickSelect}>
          {[1, 3, 5, 10].map(days => (
            <button
              key={days}
              style={{
                ...styles.quickBtn,
                ...(selectedWorkdays.length === days ? { 
                  background: 'var(--accent)', 
                  color: 'white',
                  border: '1px solid var(--accent)',
                } : {}),
              }}
              onClick={() => handleQuickSelect(days)}
            >
              {days} {days === 1 ? 'day' : 'days'}
            </button>
          ))}
          <button
            style={styles.quickBtn}
            onClick={() => handleQuickSelect(availableDays)}
          >
            All ({availableDays})
          </button>
        </div>

        {/* Month Navigation */}
        <div style={styles.monthNav}>
          <button
            style={styles.navBtn}
            onClick={() => setCurrentMonth(subMonths(currentMonth, 1))}
          >
            <ChevronLeft size={18} />
          </button>
          <span style={styles.monthLabel}>
            {format(currentMonth, 'MMMM yyyy')}
          </span>
          <button
            style={styles.navBtn}
            onClick={() => setCurrentMonth(addMonths(currentMonth, 1))}
          >
            <ChevronRight size={18} />
          </button>
        </div>

        {/* Weekday Headers */}
        <div style={styles.weekdays}>
          {['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'].map((day) => (
            <div key={day} style={styles.weekday}>{day}</div>
          ))}
        </div>

        {/* Calendar Days */}
        <div style={styles.days}>
          {calendarDays.map((day, i) => (
            <button
              key={i}
              style={getDayStyle(day)}
              onClick={() => handleDayClick(day)}
              onMouseEnter={() => startDate && !endDate && setHoverDate(day)}
              onMouseLeave={() => setHoverDate(null)}
              disabled={isBefore(day, effectiveMinDate)}
            >
              {format(day, 'd')}
            </button>
          ))}
        </div>

        {/* Summary */}
        <div style={styles.summary}>
          <div style={styles.summaryRow}>
            <span style={styles.summaryLabel}>Selected dates</span>
            <span style={styles.summaryValue}>
              {startDate ? (
                endDate || hoverDate ? (
                  `${format(startDate, 'MMM d')} - ${format(endDate || hoverDate!, 'MMM d')}`
                ) : (
                  format(startDate, 'MMM d, yyyy')
                )
              ) : (
                'Click to select'
              )}
            </span>
          </div>
          <div style={styles.summaryRow}>
            <span style={styles.summaryLabel}>Workdays</span>
            <span style={{
              ...styles.summaryValue,
              color: selectedWorkdays.length > availableDays ? 'var(--danger)' : 'var(--success)',
            }}>
              {selectedWorkdays.length} days
              {selectedWorkdays.length > availableDays && (
                <span style={{ color: 'var(--danger)', marginLeft: '0.5rem', fontSize: '0.75rem' }}>
                  (exceeds balance!)
                </span>
              )}
            </span>
          </div>
          <div style={styles.summaryRow}>
            <span style={styles.summaryLabel}>Available balance</span>
            <span style={styles.summaryValue}>{availableDays} days</span>
          </div>
        </div>

        {/* Actions */}
        <div style={styles.actions}>
          <button
            style={{ ...styles.btn, ...styles.btnSecondary }}
            onClick={() => { setStartDate(null); setEndDate(null); }}
          >
            Clear
          </button>
          <button
            style={{
              ...styles.btn,
              ...styles.btnPrimary,
              opacity: selectedWorkdays.length === 0 ? 0.5 : 1,
              cursor: selectedWorkdays.length === 0 ? 'not-allowed' : 'pointer',
            }}
            onClick={handleConfirm}
            disabled={selectedWorkdays.length === 0}
          >
            Confirm {selectedWorkdays.length > 0 && `(${selectedWorkdays.length} days)`}
          </button>
        </div>
      </div>
    </div>
  );
}
