import { useState, useMemo, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useParams } from 'react-router-dom';
import { Gift, User, Info } from 'lucide-react';
import { getEmployees, getBalance, getTransactions, getCurrentScenario } from '../api/client';
// Types are inferred from the API client functions
import { format, parseISO } from 'date-fns';

const REWARDS_RESOURCE_TYPES = [
  { id: 'wellness_points', label: 'Wellness Points', color: '#10b981', unit: 'points' },
  { id: 'learning_credits', label: 'Learning Credits', color: '#3b82f6', unit: 'dollars' },
  { id: 'recognition_points', label: 'Recognition Points', color: '#f59e0b', unit: 'points' },
  { id: 'flex_benefits', label: 'Flex Benefits', color: '#8b5cf6', unit: 'dollars' },
  { id: 'remote_days', label: 'Remote Work Days', color: '#06b6d4', unit: 'days' },
  { id: 'volunteer_hours', label: 'Volunteer Hours', color: '#ec4899', unit: 'hours' },
];

export function RewardsDashboard() {
  const { id } = useParams();
  const [selectedEmployee, setSelectedEmployee] = useState<string | null>(id || null);
  const [selectedResourceType, setSelectedResourceType] = useState('wellness_points');

  const { data: employees, isLoading: loadingEmployees } = useQuery({
    queryKey: ['employees'],
    queryFn: getEmployees,
  });

  const { data: balance, isLoading: loadingBalance } = useQuery({
    queryKey: ['balance', selectedEmployee, selectedResourceType],
    queryFn: () => getBalance(selectedEmployee!, selectedResourceType),
    enabled: !!selectedEmployee,
  });

  const { data: transactions } = useQuery({
    queryKey: ['transactions', selectedEmployee],
    queryFn: () => getTransactions(selectedEmployee!),
    enabled: !!selectedEmployee,
  });

  const { data: currentScenario } = useQuery({
    queryKey: ['currentScenario'],
    queryFn: getCurrentScenario,
  });

  // Filter resource types to only show those the employee actually has
  const availableResourceTypes = useMemo(() => {
    if (!transactions || transactions.length === 0) {
      return REWARDS_RESOURCE_TYPES.filter(rt => rt.id === 'wellness_points');
    }
    const resourceTypeSet = new Set(transactions.map(tx => tx.resource_type));
    return REWARDS_RESOURCE_TYPES.filter(rt => resourceTypeSet.has(rt.id));
  }, [transactions]);

  // Handle URL parameter and set default employee when employees load
  useEffect(() => {
    if (id && employees && employees.some(e => e.id === id)) {
      setSelectedEmployee(id);
    } else if (!selectedEmployee && employees && employees.length > 0) {
      setSelectedEmployee(employees[0].id);
    }
  }, [id, selectedEmployee, employees]);

  // Reset selected resource type when employee changes and it's not available
  useEffect(() => {
    if (availableResourceTypes.length > 0 && !availableResourceTypes.some(rt => rt.id === selectedResourceType)) {
      setSelectedResourceType(availableResourceTypes[0].id);
    }
  }, [availableResourceTypes, selectedResourceType]);

  if (loadingEmployees) {
    return <div className="loading"><div className="spinner" /> Loading...</div>;
  }

  if (!employees?.length) {
    return (
      <div className="empty-state">
        <User className="empty-state-icon" size={48} />
        <h2>No Employees</h2>
        <p>Load a rewards scenario to get started</p>
      </div>
    );
  }

  const employee = employees.find(e => e.id === selectedEmployee) || employees[0];

  return (
    <div>
      <header style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', marginBottom: '0.5rem' }}>
          <h1 style={{ fontSize: '1.5rem', fontWeight: 600 }}>Rewards Dashboard</h1>
          <select
            className="form-select"
            style={{ width: 'auto' }}
            value={selectedEmployee || ''}
            onChange={(e) => setSelectedEmployee(e.target.value)}
          >
            {employees.map(emp => (
              <option key={emp.id} value={emp.id}>{emp.name}</option>
            ))}
          </select>
        </div>
        <p style={{ color: 'var(--text-muted)', fontSize: '0.875rem' }}>
          {employee?.email} · Hired {employee?.hire_date}
        </p>
      </header>

      {/* Resource Type Tabs - Only show types employee actually has */}
      {availableResourceTypes.length > 0 && (
        <div className="tabs" style={{ marginBottom: '1.5rem' }}>
          {availableResourceTypes.map(rt => (
            <button
              key={rt.id}
              className={`tab ${selectedResourceType === rt.id ? 'active' : ''}`}
              onClick={() => setSelectedResourceType(rt.id)}
              style={{ 
                borderBottomColor: selectedResourceType === rt.id ? rt.color : 'transparent',
              }}
            >
              {rt.label}
            </button>
          ))}
        </div>
      )}

      {loadingBalance ? (
        <div className="loading"><div className="spinner" /> Loading balance...</div>
      ) : balance && balance.policies && balance.policies.length > 0 ? (
        <div>
          {/* Balance Summary */}
          <div className="balance-card" style={{ marginBottom: '1.5rem' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
              <h2 style={{ fontSize: '1.25rem', fontWeight: 600 }}>
                {REWARDS_RESOURCE_TYPES.find(rt => rt.id === selectedResourceType)?.label || 'Balance'}
              </h2>
              <div style={{ fontSize: '2rem', fontWeight: 700, color: 'var(--accent)' }}>
                {balance.total_available.toFixed(2)} {REWARDS_RESOURCE_TYPES.find(rt => rt.id === selectedResourceType)?.unit || ''}
              </div>
            </div>

            {/* Policy Breakdown */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
              {balance.policies.map(policy => (
                <div key={policy.policy_id} className="card" style={{ padding: '1rem' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
                    <h3 style={{ fontSize: '1rem', fontWeight: 600 }}>{policy.policy_name}</h3>
                    <span style={{ fontSize: '1.25rem', fontWeight: 600, color: 'var(--accent)' }}>
                      {policy.available.toFixed(2)} {REWARDS_RESOURCE_TYPES.find(rt => rt.id === selectedResourceType)?.unit || ''}
                    </span>
                  </div>
                  <div style={{ display: 'flex', gap: '1rem', fontSize: '0.875rem', color: 'var(--text-muted)' }}>
                    <span>Total: {policy.total_entitlement.toFixed(2)}</span>
                    <span>Used: {policy.consumed.toFixed(2)}</span>
                    {policy.pending > 0 && <span>Pending: {policy.pending.toFixed(2)}</span>}
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Transaction History */}
          {transactions && transactions.length > 0 && (
            <div className="card">
              <h2 style={{ fontSize: '1.25rem', fontWeight: 600, marginBottom: '1rem' }}>Transaction History</h2>
              <div className="table-container">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Date</th>
                      <th>Type</th>
                      <th>Amount</th>
                      <th>Policy</th>
                      <th>Reason</th>
                    </tr>
                  </thead>
                  <tbody>
                    {transactions
                      .filter(tx => tx.resource_type === selectedResourceType)
                      .sort((a, b) => new Date(b.effective_at).getTime() - new Date(a.effective_at).getTime())
                      .map(tx => (
                        <tr key={tx.id}>
                          <td>{format(parseISO(tx.effective_at), 'MMM d, yyyy')}</td>
                          <td>
                            <span className={`badge ${tx.type === 'accrual' ? 'badge-success' : tx.type === 'consumption' ? 'badge-danger' : 'badge-info'}`}>
                              {tx.type}
                            </span>
                          </td>
                          <td style={{ fontWeight: tx.delta > 0 ? 600 : 'normal', color: tx.delta > 0 ? 'var(--success)' : 'var(--danger)' }}>
                            {tx.delta > 0 ? '+' : ''}{tx.delta.toFixed(2)} {tx.unit}
                          </td>
                          <td>{tx.policy_id}</td>
                          <td>{tx.reason || '-'}</td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      ) : (
        <div className="empty-state">
          <Gift className="empty-state-icon" size={48} />
          <h2>No Rewards Policies</h2>
          <p>No policies are assigned for {REWARDS_RESOURCE_TYPES.find(rt => rt.id === selectedResourceType)?.label || 'this resource type'}.</p>
        </div>
      )}

      {/* Scenario Summary */}
      {currentScenario && (
        <div className="scenario-card" style={{ marginTop: '2rem', padding: '1rem', borderRadius: '8px', background: 'var(--bg-secondary)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.5rem' }}>
            <Info size={16} />
            <span style={{ fontWeight: 600 }}>Active Scenario</span>
          </div>
          <div style={{ fontSize: '0.875rem', color: 'var(--text-muted)' }}>
            <strong>{currentScenario.name}</strong>: {currentScenario.description}
          </div>
        </div>
      )}
    </div>
  );
}
