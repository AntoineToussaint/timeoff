import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom';
import { Clock, Users, FileText, Zap, Gift, Calendar, CheckSquare, RefreshCw } from 'lucide-react';
import { EmployeeDashboard } from './components/EmployeeDashboard';
import { RewardsDashboard } from './components/RewardsDashboard';
import { ScenarioLoader } from './components/ScenarioLoader';
import { HolidayAdmin } from './components/HolidayAdmin';
import { ApprovalQueue } from './components/ApprovalQueue';
import { ReconciliationAdmin } from './components/ReconciliationAdmin';
import './App.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <div className="app">
          <nav className="sidebar">
            <div className="logo">
              <Clock size={24} />
              <span>TimeOff</span>
            </div>
            <div className="nav-links">
              <NavLink to="/" end className={({ isActive }) => isActive ? 'active' : ''}>
                <Users size={20} />
                <span>Time Off</span>
              </NavLink>
              <NavLink to="/rewards" className={({ isActive }) => isActive ? 'active' : ''}>
                <Gift size={20} />
                <span>Rewards</span>
              </NavLink>
              <NavLink to="/scenarios" className={({ isActive }) => isActive ? 'active' : ''}>
                <Zap size={20} />
                <span>Scenarios</span>
              </NavLink>
              <div style={{ borderTop: '1px solid rgba(255,255,255,0.1)', margin: '12px 0' }} />
              <NavLink to="/admin/approvals" className={({ isActive }) => isActive ? 'active' : ''}>
                <CheckSquare size={20} />
                <span>Approvals</span>
              </NavLink>
              <NavLink to="/admin/holidays" className={({ isActive }) => isActive ? 'active' : ''}>
                <Calendar size={20} />
                <span>Holidays</span>
              </NavLink>
              <NavLink to="/admin/reconciliation" className={({ isActive }) => isActive ? 'active' : ''}>
                <RefreshCw size={20} />
                <span>Reconciliation</span>
              </NavLink>
            </div>
            <div className="nav-footer">
              <FileText size={16} />
              <span>Time Resource Engine</span>
            </div>
          </nav>
          <div className="content">
            <div className="content-wrapper">
              <Routes>
                <Route path="/" element={<EmployeeDashboard />} />
                <Route path="/employee/:id" element={<EmployeeDashboard />} />
                <Route path="/rewards" element={<RewardsDashboard />} />
                <Route path="/rewards/:id" element={<RewardsDashboard />} />
                <Route path="/scenarios" element={<ScenarioLoader />} />
                <Route path="/admin/approvals" element={<ApprovalQueue />} />
                <Route path="/admin/holidays" element={<HolidayAdmin />} />
                <Route path="/admin/reconciliation" element={<ReconciliationAdmin />} />
              </Routes>
            </div>
          </div>
        </div>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

export default App;
