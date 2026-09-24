import { Github, Home } from 'lucide-react';
import { HeaderSettings } from '../components/layout/HeaderSettings';
import { AppLogo } from '../components/shared/AppLogo';
import { AuthPanel, type AuthPanelProps } from '../features/auth/AuthPanel';
import { useText } from '../locales';
import '../styles/auth.css';
import '../styles/landing.css';
import '../styles/layout.css';
import '../styles/settings.css';

export function LoginPage({ onDone, initialMode = 'login' }: AuthPanelProps) {
  const text = useText();
  return (
    <div className={'landing-page login-page'}>
      <a href={'#auth-panel'} className="skip-to-content">
        {text.login.skipToContent ?? '跳到主要内容'}
      </a>
      <header className="landing-header">
        <div className="landing-brand">
          <span className="app-header-brand-mark">
            <AppLogo />
          </span>
          <span>HLOOL Mail</span>
        </div>
        <nav className="landing-nav">
          {
            <a
              className="landing-nav-link landing-nav-icon"
              href="#/"
              aria-label={text.login.homeLink || 'Home'}
            >
              <Home size={18} />
            </a>
          }
          <a
            className="landing-nav-link landing-nav-icon"
            href="https://github.com/hloolx/HloolMail"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="GitHub"
          >
            <Github size={17} />
          </a>
        </nav>
        <HeaderSettings />
      </header>

      <main className={'login-main'}>
        {<AuthPanel onDone={onDone} initialMode={initialMode} />}
      </main>
    </div>
  );
}
