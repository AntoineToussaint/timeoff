import React, { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { 
  Zap, Check, RefreshCw, AlertCircle,
  UserPlus, CalendarClock, RotateCcw, Layers, Heart, Gift, Clock
} from 'lucide-react';
import { getScenarios, loadScenario, resetDatabase } from '../api/client';
import type { Scenario } from '../api/client';

// Scenario details configuration
const scenarioDetails: Record<string, { icon: React.ReactNode; color: string; features: string[] }> = {
  'new-employee': {
    icon: <UserPlus size={22} strokeWidth={1.5} />,
    color: '#10b981',
    features: ['Single PTO policy', '20 days/year', 'Monthly accruals', 'ConsumeAhead mode'],
  },
  'mid-year-hire': {
    icon: <CalendarClock size={22} strokeWidth={1.5} />,
    color: '#3b82f6',
    features: ['Hired June 15', 'Prorated accruals', 'Partial year balance', 'Shows proration'],
  },
  'year-end-rollover': {
    icon: <RotateCcw size={22} strokeWidth={1.5} />,
    color: '#f59e0b',
    features: ['Full year accruals', '15 days remaining', 'Max 10 day carryover', 'Ready to rollover'],
  },
  'multi-policy': {
    icon: <Layers size={22} strokeWidth={1.5} />,
    color: '#8b5cf6',
    features: ['3 PTO policies', 'Priority ordering', 'Carryover + Bonus + Standard', 'Plus sick leave'],
  },
  'new-parent': {
    icon: <Heart size={22} strokeWidth={1.5} />,
    color: '#ec4899',
    features: ['Maternity (12 weeks)', 'PTO + Sick leave', 'Floating holidays', '5 resource types'],
  },
  'rewards-benefits': {
    icon: <Gift size={22} strokeWidth={1.5} />,
    color: '#06b6d4',
    features: ['Wellness points', 'Learning credits', 'Recognition kudos', 'Flex benefits', 'WFH days'],
  },
  'hourly-worker': {
    icon: <Clock size={22} strokeWidth={1.5} />,
    color: '#64748b',
    features: ['Earn-then-use mode', 'Approval required', 'Auto-approve < 1 day', 'Monthly accruals'],
  },
};

export function ScenarioLoader() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [selectedScenario, setSelectedScenario] = useState<string | null>(null);
  const [loadedScenario, setLoadedScenario] = useState<string | null>(null);

  const { data: scenarios, isLoading } = useQuery({
    queryKey: ['scenarios'],
    queryFn: getScenarios,
  });

  const loadMutation = useMutation({
    mutationFn: loadScenario,
    onSuccess: async (_, scenarioId) => {
      setLoadedScenario(scenarioId);
      // Wait for queries to refetch before navigating
      await queryClient.refetchQueries();
      
      // Redirect based on scenario category
      const scenario = scenarios?.find(s => s.id === scenarioId);
      if (scenario?.category === 'rewards') {
        navigate('/rewards');
      } else {
        navigate('/');
      }
    },
  });

  const resetMutation = useMutation({
    mutationFn: resetDatabase,
    onSuccess: () => {
      setLoadedScenario(null);
      queryClient.invalidateQueries();
    },
  });

  if (isLoading) {
    return <div className="loading"><div className="spinner" /> Loading scenarios...</div>;
  }

  return (
    <div>
      <header style={{ marginBottom: '2rem' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div>
            <h1 style={{ fontSize: '1.5rem', fontWeight: 600, marginBottom: '0.5rem' }}>
              Demo Scenarios
            </h1>
            <p style={{ color: 'var(--text-muted)', fontSize: '0.875rem' }}>
              Load a preset scenario to explore different resource management configurations
            </p>
          </div>
          <button
            className="btn btn-secondary"
            onClick={() => resetMutation.mutate()}
            disabled={resetMutation.isPending}
          >
            <RefreshCw size={16} />
            Reset All
          </button>
        </div>
      </header>

      {loadedScenario && (
        <div className="card" style={{ marginBottom: '1.5rem', background: 'rgba(16, 185, 129, 0.1)', borderColor: 'var(--success)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
            <Check size={20} style={{ color: 'var(--success)' }} />
            <span>
              <strong>{scenarios?.find(s => s.id === loadedScenario)?.name}</strong> scenario loaded successfully!
              <span style={{ color: 'var(--text-muted)', marginLeft: '0.5rem' }}>
                Go to Employees to see the data.
              </span>
            </span>
          </div>
        </div>
      )}

      {loadMutation.error && (
        <div className="card" style={{ marginBottom: '1.5rem', background: 'rgba(239, 68, 68, 0.1)', borderColor: 'var(--danger)' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', color: 'var(--danger)' }}>
            <AlertCircle size={20} />
            <span>{(loadMutation.error as Error).message}</span>
          </div>
        </div>
      )}

      {/* Group scenarios by category */}
      {(() => {
        const timeoffScenarios = scenarios?.filter(s => !s.category || s.category === 'timeoff') || [];
        const rewardsScenarios = scenarios?.filter(s => s.category === 'rewards') || [];
        
        return (
          <>
            {timeoffScenarios.length > 0 && (
              <div style={{ marginBottom: '2rem' }}>
                <h2 style={{ fontSize: '1.125rem', fontWeight: 600, marginBottom: '1rem', color: 'var(--text-muted)' }}>
                  Time Off Scenarios
                </h2>
                <div className="grid grid-2">
                  {timeoffScenarios.map(scenario => (
                    <ScenarioCard
                      key={scenario.id}
                      scenario={scenario}
                      isSelected={selectedScenario === scenario.id}
                      isLoaded={loadedScenario === scenario.id}
                      onClick={() => setSelectedScenario(scenario.id)}
                    />
                  ))}
                </div>
              </div>
            )}
            
            {rewardsScenarios.length > 0 && (
              <div>
                <h2 style={{ fontSize: '1.125rem', fontWeight: 600, marginBottom: '1rem', color: 'var(--text-muted)' }}>
                  Rewards Scenarios
                </h2>
                <div className="grid grid-2">
                  {rewardsScenarios.map(scenario => (
                    <ScenarioCard
                      key={scenario.id}
                      scenario={scenario}
                      isSelected={selectedScenario === scenario.id}
                      isLoaded={loadedScenario === scenario.id}
                      onClick={() => setSelectedScenario(scenario.id)}
                    />
                  ))}
                </div>
              </div>
            )}
          </>
        );
      })()}

      {selectedScenario && (
        <div style={{ marginTop: '2rem', display: 'flex', justifyContent: 'center' }}>
          <button
            className="btn btn-primary"
            onClick={() => loadMutation.mutate(selectedScenario)}
            disabled={loadMutation.isPending}
            style={{ padding: '0.75rem 2rem' }}
          >
            {loadMutation.isPending ? (
              <>
                <div className="spinner" style={{ width: 16, height: 16 }} />
                Loading...
              </>
            ) : (
              <>
                <Zap size={18} />
                Load {scenarios?.find(s => s.id === selectedScenario)?.name} Scenario
              </>
            )}
          </button>
        </div>
      )}
    </div>
  );
}

function ScenarioCard({ 
  scenario, 
  isSelected, 
  isLoaded, 
  onClick 
}: { 
  scenario: Scenario; 
  isSelected: boolean; 
  isLoaded: boolean;
  onClick: () => void;
}) {
  const details = scenarioDetails[scenario.id] || { 
    icon: <Layers size={22} strokeWidth={1.5} />, 
    color: 'var(--text-muted)', 
    features: [] 
  };

  return (
    <div
      className={`scenario-card ${isSelected ? 'active' : ''}`}
      onClick={onClick}
      style={{
        borderLeft: `3px solid ${details.color}`,
        position: 'relative',
      }}
    >
      {isLoaded && (
        <div style={{
          position: 'absolute',
          top: '0.75rem',
          right: '0.75rem',
        }}>
          <span className="badge badge-success">
            <Check size={12} /> Active
          </span>
        </div>
      )}

      <div style={{ display: 'flex', gap: '1rem', alignItems: 'flex-start' }}>
        <div style={{
          width: '40px',
          height: '40px',
          borderRadius: '10px',
          background: `${details.color}15`,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: details.color,
          flexShrink: 0,
        }}>
          {details.icon}
        </div>
        <div style={{ flex: 1 }}>
          <h3 style={{ 
            marginBottom: '0.25rem', 
            paddingRight: isLoaded ? '70px' : 0,
            fontSize: '0.9375rem',
            fontWeight: 600,
          }}>
            {scenario.name}
          </h3>
          <p style={{ 
            marginBottom: '0.75rem',
            fontSize: '0.8125rem',
            color: 'var(--text-muted)',
            lineHeight: 1.4,
          }}>
            {scenario.description}
          </p>
          
          <div style={{ 
            display: 'flex', 
            flexWrap: 'wrap', 
            gap: '0.375rem',
          }}>
            {details.features.map((feature, i) => (
              <span
                key={i}
                style={{
                  padding: '0.2rem 0.5rem',
                  background: 'var(--bg-dark)',
                  borderRadius: '4px',
                  fontSize: '0.6875rem',
                  color: 'var(--text-muted)',
                  border: '1px solid var(--border)',
                }}
              >
                {feature}
              </span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
